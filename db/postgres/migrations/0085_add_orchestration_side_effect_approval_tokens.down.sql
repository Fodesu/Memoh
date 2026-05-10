-- 0085_add_orchestration_side_effect_approval_tokens
-- Remove fenced approval tokens for irreversible orchestration side effects.

DROP TABLE IF EXISTS orchestration_side_effect_approval_tokens;
