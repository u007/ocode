---
type: Gotcha
title: Session Storage Critical Issues
description: Critical issues discovered in session storage code review
timestamp: 2026-09-08T07:51:32Z
---
## 5. Message.OpenAIResponseRoute Not Serialized in Legacy .ojsonl Path

**Impact:** Reloads can lose the OpenAIResponseRoute and mishandle encrypted reasoning replay.

**Details:** The legacy `.ojsonl` session serialization path does not serialize `Message.OpenAIResponseRoute`, which carries the routing information needed for encrypted reasoning replay. When a session is reloaded from `.ojsonl` format, this route information is lost, causing the system to mishandle encrypted reasoning sessions. The route must be preserved through the round-trip between SQLite and `.ojsonl` formats to ensure proper replay behavior.

**Resolution:** Update the `.ojsonl` serializer to include `Message.OpenAIResponseRoute` in the output, and ensure the SQLite deserializer can round-trip this field. Add validation to detect missing routes on reload and trigger a re-derivation path if needed. Coverage must include both SQLite→.ojsonl and .ojsonl→SQLite directions to prevent silent data loss.