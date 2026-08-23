---
name: livewire-components
description: Reactive server-driven components for Nimbus Livewire — wire:* directives, .nimbus templates, state synchronization, lifecycle hooks, and DOM diffing.
---

# Nimbus Livewire Expert

Guidance for creating reactive, interactive frontend user interfaces in Go using `nimbus-livewire`.

## Core Concepts

1. **Component Definition**:
   - Embed `livewire.Component` in your Go struct:
     ```go
     type CounterComponent struct {
         livewire.Component
         Count int `livewire:"prop"`
     }

     func (c *CounterComponent) Increment() {
         c.Count++
     }

     func (c *CounterComponent) Render() string {
         return `<div class="counter">
             <button wire:click="Increment">+</button>
             <span>{{ .Count }}</span>
         </div>`
     }
     ```

2. **Template Directives (`.nimbus`)**:
   - `wire:click="methodName"`: Dispatches actions to server component.
   - `wire:model="fieldName"`: Two-way binds form input values.
   - `wire:loading`: Displays loading states during server round-trips.
   - `wire:target="action"`: Targets loading state to a specific action.

3. **Lifecycle Hooks**:
   - `Mount(params map[string]any)`: Runs once when component initializes.
   - `Updated(prop string, val any)`: Fires when client updates a property.
