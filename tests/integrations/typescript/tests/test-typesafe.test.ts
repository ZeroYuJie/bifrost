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
