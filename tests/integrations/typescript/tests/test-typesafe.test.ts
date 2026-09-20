/**
 * Typesafe Integration Tests - Official SDK against Bifrost
 *
 * 🌉 SDK DROP-IN TESTING:
 * Uses the official TypeSafe JavaScript SDK (@typesafe-ai/sdk) pointed at
 * Bifrost's /typesafe prefix via baseURL, authenticating to Bifrost with the
 * suite's virtual key via the x-bf-vk header. Every call goes through the SDK.
 */

import { BadRequestError, choice, noul, score, TypeSafeClient } from "@typesafe-ai/sdk";
import { beforeAll, describe, expect, it } from "vitest";
import { getVirtualKey, isVirtualKeyConfigured } from "../src/utils/config-loader";

const STATE =
	"Customer message: I was double charged last month and nobody replied to my two emails. I want a refund today or I am cancelling.";

let client: TypeSafeClient;

beforeAll(() => {
	const baseUrl = process.env.BIFROST_BASE_URL || "http://localhost:8080";
	const vk = isVirtualKeyConfigured() ? getVirtualKey() : "";
	client = new TypeSafeClient({
		baseURL: `${baseUrl}/typesafe`,
		apiKey: vk || "dummy-key-bifrost-injects-the-real-one",
		defaultHeaders: vk ? { "x-bf-vk": vk } : undefined,
	});
});

describe("Typesafe SDK systemOne", () => {
	it("answers all three question types", async () => {
		const response = await client.systemOne({
			state: STATE,
			model: "jev-1.13.0",
			questions: {
				is_frustrated: noul("Is the customer frustrated?"),
				category: choice("Pick the ticket category", {
					billing: "charges and refunds",
					bug: "product defects",
					other: "anything else",
				}),
				urgency: score("Rate how urgently this needs a human reply", [
					"can wait a week",
					"should be answered soon",
					"needs a reply today",
				]),
			},
		});

		expect(response.model).toBe("jev-1.13.0");
		expect(response.answers.is_frustrated.noul).toBeGreaterThanOrEqual(0);
		expect(response.answers.is_frustrated.noul).toBeLessThanOrEqual(1);
		expect(["billing", "bug", "other"]).toContain(response.answers.category.choice);
		expect(typeof response.answers.urgency.score).toBe("number");
		expect(response.usage.input_tokens).toBeGreaterThan(0);
	}, 60000);

	it("resolves a model alias to its versioned id", async () => {
		const response = await client.systemOne({
			state: "Reply: Sure, sounds good, see you at 3pm.",
			model: "jev-latest",
			questions: { is_confirmation: noul("Does this reply confirm the meeting?") },
		});
		expect(response.model).toMatch(/^jev-/);
		expect(response.model).not.toBe("jev-latest");
	}, 60000);
});

describe("Typesafe SDK models", () => {
	it("lists jev models with bare names", async () => {
		// The JS SDK returns the model array directly (unlike the Python SDK's
		// wrapper object).
		const listing = await client.models.list();
		const names = listing.map((m) => m.name);
		expect(names).toContain("jev-1.13.0");
		expect(names).toContain("jev-latest");
		for (const name of names) expect(name).not.toContain("typesafe/");
	}, 30000);
});

describe("Typesafe SDK errors", () => {
	it("parses Bifrost's native error body into a BadRequestError", async () => {
		// A choice question with an empty criteria map is rejected before dispatch;
		// the SDK must parse the native {"detail": {...}} body into its 400 type,
		// exactly as it would against api.typesafe.ai.
		await expect(
			client.systemOne({
				state: STATE,
				model: "jev-1.13.0",
				questions: { category: choice("Pick one", {}) },
			}),
		).rejects.toBeInstanceOf(BadRequestError);
	}, 30000);
});

// LLM fallback / emulation: any tool-capable chat model answers a decision
// request natively (primary) or as a fallback, exercised through the SDK
// (native endpoint) and the Bifrost-native /v1/decisions API (fallback).
const LLM_DECISION_MODEL = "openai/gpt-4o-mini";

describe("Decision LLM emulation", () => {
	it("an LLM emulates the decision as the primary model", async () => {
		const response = await client.systemOne({
			state: STATE,
			model: LLM_DECISION_MODEL,
			questions: {
				is_frustrated: noul("Is the customer frustrated?"),
				category: choice("Pick the ticket category", {
					billing: "charges and refunds",
					bug: "product defects",
					other: "anything else",
				}),
				urgency: score("Rate urgency", ["low", "medium", "high"]),
			},
		});
		expect(response.answers.is_frustrated.noul).toBeGreaterThanOrEqual(0);
		expect(response.answers.is_frustrated.noul).toBeLessThanOrEqual(1);
		expect(["billing", "bug", "other"]).toContain(response.answers.category.choice);
		expect(typeof response.answers.urgency.score).toBe("number");
	}, 60000);

	it("falls through to an LLM fallback when the primary fails", async () => {
		// /v1/decisions is Bifrost's native format (no upstream SDK) - HTTP directly.
		const baseUrl = process.env.BIFROST_BASE_URL || "http://localhost:8080";
		const headers: Record<string, string> = { "Content-Type": "application/json" };
		if (isVirtualKeyConfigured()) headers["x-bf-vk"] = getVirtualKey();
		const response = await fetch(`${baseUrl}/v1/decisions`, {
			method: "POST",
			headers,
			body: JSON.stringify({
				model: "openai/nonexistent-model-xyz",
				fallbacks: [LLM_DECISION_MODEL],
				state: "Reply: sounds good, see you at 3pm.",
				questions: { is_confirmation: { kind: "noul", instructions: "Does this confirm the meeting?" } },
			}),
		});
		const body = await response.json();
		expect(response.status, JSON.stringify(body)).toBe(200);
		expect(body.answers.is_confirmation.kind).toBe("noul");
		expect(body.answers.is_confirmation.value).toBeGreaterThanOrEqual(0);
		expect(body.answers.is_confirmation.value).toBeLessThanOrEqual(1);
	}, 60000);

	it("reports the difference between native jev and LLM emulation", async () => {
		const questions = {
			is_frustrated: noul("Is the customer frustrated?"),
			category: choice("Pick the ticket category", {
				billing: "charges and refunds",
				bug: "product defects",
				other: "anything else",
			}),
			urgency: score("Rate urgency", ["low", "medium", "high"]),
		};
		const native = await client.systemOne({ state: STATE, model: "jev-1.13.0", questions });
		const emulated = await client.systemOne({ state: STATE, model: LLM_DECISION_MODEL, questions });

		const read = (r: any, name: string) => {
			const k = r.answers[name].type;
			return k === "noul" ? r.answers[name].noul : k === "choice" ? r.answers[name].choice : r.answers[name].score;
		};
		console.log("\n=== Decision: native jev vs LLM emulation ===");
		for (const name of Object.keys(questions)) {
			const nv = read(native, name);
			const ev = read(emulated, name);
			console.log(`${name}: jev=${nv} gpt-4o-mini=${ev} ${nv === ev ? "=" : "DIFF"}`);
			expect(native.answers[name].type).toBe(emulated.answers[name].type);
		}
		expect(native.model).toMatch(/^jev-/);
		expect(emulated.model).not.toBe(native.model);
	}, 60000);
});
