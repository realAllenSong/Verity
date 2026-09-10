# Privacy boundary

The demo encodes the intended boundary, but it is not a completed enterprise security system.

"Local" means the machine running the Verity API. With localhost deployment that is your laptop; with a cluster deployment uploaded bytes are stored on the server PVC. The workbench shows a protected projection of raw and before/after content to its trusted operator. Explicitly private fragments are replaced with a structural placeholder; credentials, email addresses, phone numbers and common bearer/token fields are redacted recursively. This baseline is not a DLP system and does not replace field-level authorization. Do not upload real employee, credential, or confidential content to an untrusted or shared deployment. Curated stage JSONL and text columns in CSV/Parquet may contain non-sensitive source context and remain subject to policy review.

## Local-only by default

- raw prompts and model responses
- source code and full agent traces
- email, Slack, document, and presentation bodies
- local credentials, refresh tokens, and source identifiers
- direct personal identifiers

These inputs should be processed inside the user's approved local or firm-managed runtime. Credential discovery must never copy tokens into the Verity cloud service. Production connectors should use the host application's supported authentication and consent mechanisms; reading another tool's credential file directly is not a safe universal integration strategy.

## Shareable after policy checks

- pseudonymous actor and workspace IDs
- normalized event type and timestamp
- approved derived record
- confidence and evidence count
- policy and quality decisions
- operator version, input snapshot, and lineage IDs
- aggregate spend, outcome, and adoption metrics when authorized

## Explicit exclusions

The platform should not become employee surveillance. Prompt counts, token counts, online time, or raw communication content are not standalone performance ratings. Team views should emphasize workflow outcomes, cost-to-outcome, evidence quality, coaching opportunities, and uncertainty. Individual evidence must be purpose-limited, access-controlled, explainable, and contestable.

Before production use, add threat modeling, data classification, retention and deletion policies, tenant isolation, encryption, audit logging, legal review, works-council or employee consultation where applicable, and red-team tests for inference attacks on derived signals.
