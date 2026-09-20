"""
Typesafe Integration Tests - Official SDK against Bifrost

🌉 SDK DROP-IN TESTING:
This test suite uses the official TypeSafe Python SDK (typesafe-sdk) pointed at
Bifrost's /typesafe prefix via base_url, so a client written against
api.typesafe.ai must work unchanged through Bifrost. Every call in this file
goes through the SDK - no raw HTTP.

Covered scenarios:
1. system_one with all three question types (Noul, Choice, Score)
2. Structured (JSON) state and per-answer metadata (confidence, probabilities, legend)
3. Model alias resolution (jev-latest resolves to the versioned model)
4. client.models.list() against Bifrost's synthesized native listing
5. SDK exception parsing of Bifrost's native error body (TypeSafeBadRequestError)
"""

import pytest
import requests
from typesafe_sdk import (
    Choice,
    Noul,
    Score,
    TypeSafeBadRequestError,
    TypeSafeClient,
)

from .utils.common import get_bifrost_base_url
from .utils.config_loader import get_config

STATE = (
    "Customer message: I was double charged last month and nobody replied "
    "to my two emails. I want a refund today or I am cancelling."
)


@pytest.fixture
def typesafe_client():
    """Official TypeSafe SDK client pointed at Bifrost's /typesafe drop-in.

    Authenticates to Bifrost with the suite's virtual key via the x-bf-vk
    header (the cross-provider convention); Bifrost injects the real upstream
    key, so the SDK's own api_key never reaches TypeSafe.
    """
    config = get_config()
    headers = {}
    vk = config.get_virtual_key() if config.is_virtual_key_configured() else ""
    if vk:
        headers["x-bf-vk"] = vk
    client = TypeSafeClient(
        base_url=f"{get_bifrost_base_url()}/typesafe",
        api_key=vk or "dummy-key-bifrost-injects-the-real-one",
        headers=headers or None,
    )
    yield client
    client.close()


class TestTypesafeSystemOne:
    def test_01_all_question_types(self, typesafe_client):
        result = typesafe_client.system_one(
            STATE,
            {
                "is_frustrated": Noul(instructions="Is the customer frustrated?"),
                "category": Choice(
                    instructions="Pick the ticket category",
                    criteria={"billing": "charges and refunds", "bug": "product defects", "other": "anything else"},
                ),
                "urgency": Score(
                    instructions="Rate how urgently this needs a human reply",
                    criteria=["can wait a week", "should be answered soon", "needs a reply today"],
                ),
            },
            model="jev-1.13.0",
        )

        assert result.model == "jev-1.13.0"
        assert 0.0 <= result.nouls["is_frustrated"].noul <= 1.0
        assert result.choices["category"].choice in {"billing", "bug", "other"}
        assert isinstance(result.scores["urgency"].score, (int, float))
        assert result.usage.input_tokens > 0

    def test_02_structured_state_and_answer_metadata(self, typesafe_client):
        result = typesafe_client.system_one(
            {"ticket": {"id": 4211, "body": "The export button crashes the app every time."}, "user_tier": "pro"},
            {
                "area": Choice(
                    instructions="Which product area does the complaint target?",
                    criteria={"camera": "capture", "stability": "crashes", "support": "service"},
                ),
                "priority": Score(
                    instructions="Bug backlog rank?",
                    criteria=["backlog", "next sprint", "this sprint", "hotfix now"],
                ),
            },
            model="jev-1.13.0",
        )

        area = result.choices["area"]
        assert area.choice in {"camera", "stability", "support"}
        assert area.probabilities is not None and abs(sum(area.probabilities.values()) - 1.0) < 0.05
        priority = result.scores["priority"]
        assert priority.legend is not None and len(priority.legend) == 4

    def test_03_model_alias_resolves(self, typesafe_client):
        result = typesafe_client.system_one(
            "Reply: Sure, sounds good, see you at 3pm.",
            {"is_confirmation": Noul(instructions="Does this reply confirm the meeting?")},
            model="jev-latest",
        )
        # Aliases resolve upstream; the response reports the versioned model.
        assert result.model.startswith("jev-")
        assert result.model != "jev-latest"


class TestTypesafeModels:
    def test_01_models_list(self, typesafe_client):
        listing = typesafe_client.models.list()
        names = [m.name for m in listing.models]
        assert "jev-1.13.0" in names
        assert "jev-latest" in names
        assert all("typesafe/" not in name for name in names)


class TestTypesafeErrors:
    def test_01_bad_request_parses_native_error(self, typesafe_client):
        # A choice question with empty criteria is rejected by Bifrost before
        # dispatch; the SDK must parse the native {"detail": {...}} body into
        # its 400 exception type exactly as it would against api.typesafe.ai.
        with pytest.raises(TypeSafeBadRequestError) as excinfo:
            typesafe_client.system_one(
                STATE,
                {"category": Choice(instructions="Pick one", criteria={})},
                model="jev-1.13.0",
            )
        assert "criteria" in str(excinfo.value)


# LLM fallback / emulation: any tool-capable chat model answers a decision
# request natively (primary) or as a fallback. Exercised through the official
# TypeSafe SDK pointed at an LLM model - the /typesafe drop-in stays the client.
LLM_DECISION_MODEL = "openai/gpt-4o-mini"


class TestDecisionLLMEmulation:
    def test_01_llm_primary_emulation(self, typesafe_client):
        result = typesafe_client.system_one(
            STATE,
            {
                "is_frustrated": Noul(instructions="Is the customer frustrated?"),
                "category": Choice(
                    instructions="Pick the ticket category",
                    criteria={"billing": "charges and refunds", "bug": "product defects", "other": "anything else"},
                ),
                "urgency": Score(
                    instructions="Rate urgency",
                    criteria=["low", "medium", "high"],
                ),
            },
            model=LLM_DECISION_MODEL,
        )
        # An LLM emulates the judgment via tool-calling; answers come back in the
        # native shape with the LLM-estimated confidence.
        assert 0.0 <= result.nouls["is_frustrated"].noul <= 1.0
        assert result.choices["category"].choice in {"billing", "bug", "other"}
        assert isinstance(result.scores["urgency"].score, (int, float))

    def test_02_llm_fallback(self):
        # Primary model fails at the provider; the request falls through to the
        # LLM fallback which emulates the decision. /v1/decisions is Bifrost's
        # native format (no upstream SDK), so this uses the HTTP API directly.
        config = get_config()
        headers = {"Content-Type": "application/json"}
        if config.is_virtual_key_configured():
            headers["x-bf-vk"] = config.get_virtual_key()
        response = requests.post(
            f"{get_bifrost_base_url()}/v1/decisions",
            headers=headers,
            json={
                "model": "openai/nonexistent-model-xyz",
                "fallbacks": [LLM_DECISION_MODEL],
                "state": "Reply: sounds good, see you at 3pm.",
                "questions": {"is_confirmation": {"kind": "noul", "instructions": "Does this confirm the meeting?"}},
            },
            timeout=60,
        )
        assert response.status_code == 200, response.text
        answer = response.json()["answers"]["is_confirmation"]
        assert answer["kind"] == "noul"
        assert 0.0 <= answer["value"] <= 1.0

    def test_03_native_vs_emulated_comparison(self, typesafe_client):
        # Same decision answered by the native jev judgment model and by an LLM
        # emulation; report how the two differ. Both must return well-formed
        # answers of the right kinds - the values themselves may differ, which is
        # the point of the comparison.
        questions = {
            "is_frustrated": Noul(instructions="Is the customer frustrated?"),
            "category": Choice(
                instructions="Pick the ticket category",
                criteria={"billing": "charges and refunds", "bug": "product defects", "other": "anything else"},
            ),
            "urgency": Score(instructions="Rate urgency", criteria=["low", "medium", "high"]),
        }
        native = typesafe_client.system_one(STATE, questions, model="jev-1.13.0")
        emulated = typesafe_client.system_one(STATE, questions, model=LLM_DECISION_MODEL)

        print("\n=== Decision: native jev vs LLM emulation ===")
        print(f"{'question':<14} {'kind':<8} {'jev-1.13.0':<16} {'gpt-4o-mini':<16} match")
        for name in questions:
            kind = native.answers[name].type
            if kind == "noul":
                nv, ev = native.nouls[name].noul, emulated.nouls[name].noul
            elif kind == "choice":
                nv, ev = native.choices[name].choice, emulated.choices[name].choice
            else:
                nv, ev = native.scores[name].score, emulated.scores[name].score
            print(f"{name:<14} {kind:<8} {str(nv):<16} {str(ev):<16} {'=' if nv == ev else 'DIFF'}")

        # Both engines answered every question in the right shape.
        for name, q in questions.items():
            assert native.answers[name].type == emulated.answers[name].type
        assert native.model.startswith("jev-")
        assert emulated.model != native.model
