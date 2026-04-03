# GoClaw Agent Events Protocol

This document defines the event streaming protocol used by the LeadAgent to communicate with clients over SSE (Server-Sent Events). It serves as a contract between the Go backend and all consuming clients (frontend, external integrations, etc.).

## Overview

The LeadAgent executes requests and emits a stream of events describing:
- **Message deltas**: Incremental text from the model (reasoning + content)
- **Tool events**: Tool invocations (input + output)
- **Task updates** (plan mode only): Task state transitions
- **Terminal events**: Run completion or error

Every event stream **must terminate** with either:
- `EventCompleted` (success, with final assistant message)
- `EventError` (failure, with error code and message)

This is the **P0 contract**: clients must always receive an explicit end-of-stream signal.

---

## Event Structure

### Event Envelope

```json
{
  "type": "message_delta|tool_event|completed|error|task_started|task_running|task_completed|task_failed",
  "thread_id": "thread-123",
  "payload": { /* event-specific data */ },
  "timestamp": 1712145600123
}
```

**Fields:**

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `type` | string | One of 8 enum values | Classifies the event |
| `thread_id` | string | Non-empty | Echoes request thread ID |
| `payload` | object | Structure depends on `type` | Type-specific data |
| `timestamp` | int64 | Unix milliseconds, UTC | ISO 8601 conversion: `new Date(ts)` |

---

## Event Types & Payloads

### 1. `message_delta`

Emitted for each incremental token/chunk from the model.

```json
{
  "type": "message_delta",
  "thread_id": "thread-123",
  "payload": {
    "content": "The answer is...",
    "is_thinking": false
  },
  "timestamp": 1712145600123
}
```

**Payload Fields:**

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `content` | string | Yes | Non-empty token or chunk |
| `is_thinking` | bool | No | `true` = internal reasoning (usually hidden from user) |

**Contract:**
- Reasoning deltas (`is_thinking=true`) should be accumulated separately from content deltas.
- Multiple deltas with `is_thinking=false` should be concatenated to form the final message.
- Frontend may render thinking content in a collapsible block or omit it.

---

### 2. `tool_event`

Emitted when a tool is invoked or returns a result. Follows a **two-phase protocol**:

#### Phase 1: Tool Invocation

```json
{
  "type": "tool_event",
  "thread_id": "thread-123",
  "payload": {
    "call_id": "call_abc123",
    "tool_name": "bash",
    "input": "{\"command\": \"ls -la /home\"}",
    "output": "",
    "is_error": false
  },
  "timestamp": 1712145600124
}
```

**When:** Emitted when the model decides to call a tool.
**Constraints:**
- `call_id`: Unique within the thread, identifies this tool invocation.
- `tool_name`: Non-empty; must match a registered tool name.
- `input`: JSON-encoded arguments (may be empty string if tool accepts no args).
- `output`: Always empty in Phase 1.
- `is_error`: Always `false` in Phase 1.

#### Phase 2: Tool Result

```json
{
  "type": "tool_event",
  "thread_id": "thread-123",
  "payload": {
    "call_id": "call_abc123",
    "tool_name": "bash",
    "input": "",
    "output": "total 48\ndrwxr-xr-x  2 root root 4096 ...",
    "is_error": false
  },
  "timestamp": 1712145600125
}
```

**When:** Emitted when the tool completes execution.
**Constraints:**
- `call_id`: Same as Phase 1 (allows client to match result to invocation).
- `tool_name`: Echoed from Phase 1.
- `input`: Always empty in Phase 2.
- `output`: Non-empty result (success) or error message (failure).
- `is_error`: `true` if output represents an error, `false` otherwise.

**Contract:**
- Phase 1 and Phase 2 must share the same `call_id`.
- Frontend uses `call_id` to correlate invocation and result.
- If a tool execution is interrupted (no Phase 2), the client should handle timeout or explicit cancellation.

---

### 3. `completed`

Emitted when the run ends successfully.

```json
{
  "type": "completed",
  "thread_id": "thread-123",
  "payload": {
    "final_message": "The answer is 42."
  },
  "timestamp": 1712145600200
}
```

**Payload Fields:**

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `final_message` | string | Non-empty (or empty if model produced no text) | Concatenation of all non-thinking deltas |

**Contract:**
- Marks the terminal success state.
- No further events follow this event.
- Frontend must clear connection and mark run as done.

---

### 4. `error`

Emitted when the run fails.

```json
{
  "type": "error",
  "thread_id": "thread-123",
  "payload": {
    "code": "agent/context_cancelled",
    "message": "context cancelled"
  },
  "timestamp": 1712145600200
}
```

**Payload Fields:**

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `code` | string | Stable error code (see below) | Machine-readable for client routing |
| `message` | string | Non-empty | Human-readable description |

**Error Codes:**

| Code | Cause | Recovery |
|------|-------|----------|
| `agent/run_error` | Agent runtime error (model API failure, tool execution crash) | Retry with same input |
| `agent/interrupted` | Run was explicitly cancelled by user | User cancellation confirmed |
| `agent/empty_stream` | Event stream unexpectedly closed | Bug; report issue |
| `agent/context_cancelled` | Request context cancelled (timeout, client disconnect) | Reconnect and resume if supported |
| `agent/not_initialized` | Agent not initialized | Server error; restart services |

**Contract:**
- Marks the terminal failure state.
- No further events follow this event.
- Frontend must display error to user and provide retry/troubleshoot options.

---

### 5. `task_started` (Plan Mode Only)

```json
{
  "type": "task_started",
  "thread_id": "thread-123",
  "payload": {
    "task_id": "task_001",
    "subject": "Research API documentation",
    "status": "pending"
  },
  "timestamp": 1712145600150
}
```

Emitted when `plan_mode=true` and a new task is created.

---

### 6. `task_running`, `task_completed`, `task_failed`

Similar structure to `task_started`, with status reflecting the transition:

```json
{
  "type": "task_completed",
  "thread_id": "thread-123",
  "payload": {
    "task_id": "task_001",
    "subject": "Research API documentation",
    "status": "completed"
  },
  "timestamp": 1712145600180
}
```

---

## SSE Framing

Events are sent as **data-only frames** in the Server-Sent Events format:

```
data: {"type":"message_delta","thread_id":"t1","payload":{"content":"..."},"timestamp":1712145600123}

data: {"type":"tool_event",...}

data: {"type":"completed",...}

```

**Notes:**
- Each event is a complete JSON object on a single line.
- The format is `data: <JSON>\n\n` (newline after `data:`, then blank line).
- Clients use standard SSE parsing libraries to receive events.

---

## Client Integration Checklist

### Receiving Events

- [ ] Open SSE connection to `POST /api/threads/{thread_id}/runs`
- [ ] Parse each `data: ...` line as JSON
- [ ] Dispatch to handler based on `type` field
- [ ] Handle all 8 event types

### Message Rendering

- [ ] Accumulate `message_delta` events with `is_thinking=false`
- [ ] Render final message when `completed` is received
- [ ] If `is_thinking=true`, optionally render in a hidden/collapsible block
- [ ] Clear message state on `error`

### Tool Tracking

- [ ] On Phase 1 `tool_event`: create a tool call entry with `call_id` and `tool_name`
- [ ] On Phase 2 `tool_event` with same `call_id`: append result
- [ ] Mark error if Phase 2 has `is_error=true`
- [ ] Timeout if Phase 2 doesn't arrive within 30s (configurable)

### Error Handling

- [ ] On `error` event: display error code + message to user
- [ ] On `agent/context_cancelled`: offer retry option
- [ ] On `agent/run_error`: suggest contacting support if retry fails
- [ ] On other errors: log for debugging

### Plan Mode

- [ ] If plan mode enabled: listen for `task_*` events
- [ ] Maintain a task list, updating status on each event
- [ ] Display task tree/progress alongside message stream

---

## Backward Compatibility & Versioning

### Current Version

- Protocol version: **1.0** (implicit in event structure)
- Timestamp format: **Unix milliseconds** (int64, not ISO 8601 string)

### Future Changes

If breaking changes are needed:
1. Add an optional `protocol_version` field to the event envelope.
2. Clients check version and route to appropriate handler.
3. Maintain support for at least two versions.

---

## Examples

### Example 1: Simple Message

```
POST /api/threads/t1/runs HTTP/1.1
Content-Type: application/json

{"input": "What is 2+2?"}

---

HTTP/1.1 200 OK
Content-Type: text/event-stream

data: {"type":"message_delta","thread_id":"t1","payload":{"content":"The answer ","is_thinking":false},"timestamp":1712145600100}

data: {"type":"message_delta","thread_id":"t1","payload":{"content":"to 2+2 is ","is_thinking":false},"timestamp":1712145600101}

data: {"type":"message_delta","thread_id":"t1","payload":{"content":"4.","is_thinking":false},"timestamp":1712145600102}

data: {"type":"completed","thread_id":"t1","payload":{"final_message":"The answer to 2+2 is 4."},"timestamp":1712145600103}

```

### Example 2: Tool Invocation

```
data: {"type":"message_delta","thread_id":"t1","payload":{"content":"Let me check the files. ","is_thinking":false},"timestamp":1712145600110}

data: {"type":"tool_event","thread_id":"t1","payload":{"call_id":"call_001","tool_name":"bash","input":"{\"command\":\"ls -la\"}","output":"","is_error":false},"timestamp":1712145600111}

data: {"type":"tool_event","thread_id":"t1","payload":{"call_id":"call_001","tool_name":"bash","input":"","output":"total 12\n-rw-r--r-- 1 user user 1024 Apr 3 12:00 file.txt","is_error":false},"timestamp":1712145600112}

data: {"type":"message_delta","thread_id":"t1","payload":{"content":"Found file.txt.","is_thinking":false},"timestamp":1712145600113}

data: {"type":"completed","thread_id":"t1","payload":{"final_message":"Found file.txt."},"timestamp":1712145600114}

```

### Example 3: Error Handling

```
data: {"type":"tool_event","thread_id":"t1","payload":{"call_id":"call_002","tool_name":"bash","input":"{\"command\":\"rm -rf /\"}","output":"","is_error":false},"timestamp":1712145600120}

data: {"type":"tool_event","thread_id":"t1","payload":{"call_id":"call_002","tool_name":"bash","input":"","output":"error: command not allowed","is_error":true},"timestamp":1712145600121}

data: {"type":"error","thread_id":"t1","payload":{"code":"agent/run_error","message":"tool execution failed: command not allowed"},"timestamp":1712145600122}

```

---

## Troubleshooting

### SSE Connection Closes Without Terminal Event

**Cause:** Server crashed or network interruption.  
**Solution:** Client should detect closed stream and offer reconnect.

### Tool Invocation Without Result (Phase 2 Missing)

**Cause:** Tool execution timed out or was interrupted.  
**Solution:** Client should timeout Phase 2 after 30s and show error state.

### Mismatched Tool `call_id`

**Cause:** Server bug or protocol violation.  
**Solution:** Log warning; assume Phase 2 doesn't match Phase 1 if ID differs.

### Empty `final_message` on Completion

**Cause:** Model produced only reasoning or no text output.  
**Solution:** Show "No response" or hide completed event; check tool results instead.

---

## References

- [Server-Sent Events (MDN)](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)
- [Eino AgentEvent](https://pkg.go.dev/github.com/cloudwego/eino@latest/adk)
- [DeerFlow Event System](../../../deer-flow/backend/CLAUDE.md)
