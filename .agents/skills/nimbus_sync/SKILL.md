---
name: nimbus_sync
description: >
  Mandatory rule to ensure that any change made to the Nimbus Go framework must be immediately 
  reflected in nimbus-starter documentation and the nimbus_expert agent skill files.
---

# Nimbus Sync Skill

This skill enforces the absolute rule that **any changes to the Nimbus framework must be kept in lockstep with the starter kit documentation and the nimbus_expert agent skill files**.

Whenever you add, modify, or remove features in the `nimbus` repository:

1. **Modify the framework code** in the `nimbus` repository.
2. **Update the corresponding documentation page** in `nimbus-starter/resources/views/docs/` (as `.nimbus` views).
3. **Update the corresponding markdown skill files** in `nimbus/.agents/skills/nimbus_expert/` (e.g. `orm.md`, `validation.md`, `routing_controllers.md`).

## Verification Workflow
- Ensure `/Users/yashkumar/Documents/Projects/nimbus` builds successfully (`go build ./...`).
- Ensure `/Users/yashkumar/Documents/Projects/nimbus-starter` builds successfully (`go build ./...`).
- Verify that there is no documentation drift or outdated code snippets in either the starter views or the agent skill files.
