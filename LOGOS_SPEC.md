# COGNITIVE OS — PROJECT SPECIFICATION (Total Scope)

**Project Title:** Futuristic Hyper-Utility Chat / Companion System
**Project Class:** Hyper-Utility Cognitive Messaging System
**Status:** Authoritative vision record — source of truth for all builds
**Owner:** Conor Ross / RINCON
**Implementation plan:** see `LOGOS.md` (tracks, architecture, verification)

---

## PRIMARY OBJECTIVE

Re-architect the core communication model from linear text threads into an
object-oriented, cognitive substrate that functions as an intelligent partner,
semantic router, and native operational toolset.

---

## 1. SYSTEM FOUNDATION: THE CONVERSATIONAL SUBSTRATE

### 1.1 Architectural Principle

- Establish the chat interface as a dynamic cognitive space rather than a
  chronological message log.
- Treat every individual message as a discrete data object equipped with
  metadata, intent, context, and inline utility.

### 1.2 Message Object Capabilities

Ensure all platform messages function as:

- **Intent-Encoded Units:** Objects carrying defined operational purpose.
- **Executable Instructions:** Native triggers for system actions.
- **Memory Anchors:** Nodes linking directly into the system's long-term
  knowledge base.
- **Semantic Nodes:** Interconnected elements within the wider conversational
  graph.
- **Adaptive Responses:** Dynamically re-rendering outputs based on contextual
  requirements.

---

## 2. INTELLIGENCE LAYER SPECIFICATIONS

### 2.1 Intent Parsing Engine

Implement an automated classification engine to tag every incoming message into
one of six core communication intents, automatically adjusting system output
tone, structure, and depth accordingly:

- **Ask:** Information-seeking queries.
- **Command:** Explicit actionable directives.
- **Reflect:** Open-ended internal thinking and processing.
- **Draft:** Requests for content creation.
- **Clarify:** Refinements, corrections, or adjustments to existing context.
- **Explore:** Lateral, unconstrained ideation.

### 2.2 Adaptive Persona Engine

Deploy a dynamic persona engine capable of adapting communication parameters
based on user style, accessible via explicit invocation or automated inference:

- **Operator Persona:** Terse, tactical, zero-fluff communication.
- **Analyst Persona:** Structured, logical, multi-layered outputs.
- **Creative Persona:** Generative, lateral, metaphorical responses.
- **Socratic Persona:** Probing, challenging, and idea-sharpening dialogue.
- **Archivist Persona:** Memory-driven, contextual, historically grounded
  responses.

### 2.3 Semantic Threading Engine

Replace standard linear messaging streams with an auto-clustering semantic
framework:

- Automatically cluster messages into semantic threads based on contextual
  relationships.
- Support manual and programmatic merging, splitting, and archiving of threads.
- Enable full-system cross-thread semantic retrieval (e.g., executing queries
  such as: "Show me everything related to [project] across all threads").
- Persist semantic threads as living, evolving documents rather than static
  historical logs.

---

## 3. UTILITY LAYER SPECIFICATIONS

### 3.1 Inline Message Transformations

Provide capabilities to transform any existing message directly within the
stream without navigating away from the chat:

- **Summary:** Condense message scope.
- **Expansion:** Elaborate on technical or contextual specifics.
- **Counterargument:** Generate oppositional perspectives.
- **Alternative Framing:** Re-cast message perspective or premise.
- **Visual Diagram:** Convert text to graphical representations.
- **Checklist:** Extract actionable tasks into interactive lists.
- **Plan:** Convert concepts into structured execution schedules.
- **Persona Rewrite:** Re-process text through a selected persona filter.

### 3.2 Native Conversational Tools

Integrate functional utilities directly into the conversation stream,
eliminating menus and UI clutter by responding to natural language prompts:

- **Rewrite:** Modify tone, length, and overall clarity.
- **Condense:** Reduce content to fundamental essentials.
- **Extract:** Pull key insights directly from text blocks.
- **Invert:** Reverse logic or perspectives.
- **Simulate:** Model and generate multi-party dialogues.
- **Draft:** Compose outbound communications.
- **Diagnose:** Identify internal ambiguities or logical weaknesses.

### 3.3 Conversational Memory Graph

Construct and update a continuous memory graph tracking the user's operational
footprint:

- **Tracked Parameters:** User preferences, communication tone, cognitive
  patterns, recurring topics, long-term project contexts, mentioned
  entities/people, and operational style.
- **Query Capabilities:** Support direct conversational queries against the
  memory graph (e.g., "What communication patterns do I use most?", "How has my
  thinking evolved on this topic?", "Show me my last five strategic decisions.").

---

## 4. USER INTERFACE & EXPERIENCE (UI/UX) SPECIFICATIONS

### 4.1 Interface Design

- Implement a black and off-white, high-contrast, operator-grade UI.
- Omit decorative UI elements: no speech bubbles, rounded corners, emojis, or
  gradients.
- Focus layout strictly on clean text presentation, semantic indicators, and
  precision utilities.

### 4.2 Visual Message States

Provide visual cues for every message via distinct interface markers:

- **Intent Color Band:** Visual code indicating classified message intent.
- **Persona Tag:** Marker identifying the active persona mode.
- **Confidence Indicator:** Metric displaying output reliability/certainty.
- **Thread Affiliation:** Visual link to the associated semantic thread.
- **Execution Marker:** Indicator showing action execution status.

### 4.3 Non-Modal Inline Utilities

Integrate lightweight hover and tap state controls directly on message elements
to trigger actions without opening modal overlay windows:

- Transform controls
- Summarization triggers
- Expansion triggers
- Direct thread navigation links
- Memory graph references
- Persona switching controls

---

## 5. OPERATIONAL BEHAVIOR DIRECTIVES

### 5.1 Proactive Clarification Protocol

Program the system to actively identify communication ambiguities and prompt the
user for immediate clarification prior to processing. Direct query examples:

- "Do you mean X or Y?"
- "Should I treat this as a command or exploration?"
- "Is this part of the same thread?"

### 5.2 Cognitive Mirroring Architecture

Ensure system response characteristics dynamically adapt to match user inputs:

- **Terse Inputs:** Yield concise, high-density outputs.
- **Analytical Inputs:** Yield structured, logically broken-down frameworks.
- **Brainstorming Inputs:** Yield generative, exploratory outputs.

### 5.3 Conversation Sculpting Capabilities

Provide features allowing the system to actively re-structure unstructured
inputs:

- Merge related message streams.
- Extract clean summary blocks.
- Construct structured outlines from scattered dialogue.
- Detect and highlight internal logic contradictions.
- Extract and emphasize key operational insights.

---

## 6. TECHNICAL DATA SCHEMAS & MODULE SPECIFICATIONS

### 6.1 Required Core System Modules

- **Intent Parser:** Classifies incoming inputs into functional communication
  buckets.
- **Persona Engine:** Manages dynamic system persona parameters.
- **Semantic Thread Manager:** Handles auto-clustering, thread life cycles, and
  cross-thread retrieval.
- **Message Transformer:** Executes inline content manipulations.
- **Memory Graph:** Maintained database tracking user operational patterns and
  history.
- **Inline Utility Toolkit:** Executes conversational tools without UI overhead.
- **Adaptive Response Generator:** Formulates outputs based on intent, persona,
  and memory parameters.

### 6.2 Message Object Data Schema

Every message object within the database must conform to the following schema:

```json
{
  "content": "String",
  "intent": "String",
  "persona": "String",
  "context": "Object / String",
  "thread_id": "String",
  "confidence": "Float",
  "metadata": "Object",
  "transform_history": "Array"
}
```

### 6.3 Required Companion Behavior Matrix

- **Adaptive Tone Engine:** Active tone shift based on user input analysis.
- **Proactive Clarification:** Automated ambiguity resolution loops.
- **Semantic Routing:** Context-aware routing of incoming messages to relevant
  threads.
- **Memory-Aware Responses:** Direct insertion of historical graph context into
  active outputs.
- **Thread Management:** Automatic and manual clustering, merging, and splitting
  of conversation paths.
- **Inline Transformations:** Real-time, non-destructive editing of content
  objects.

### 6.4 UI Component Architecture Specs

- Minimalist operator-grade high-contrast layout.
- Semantic color-band rendering module.
- Inline utility hover state triggers.
- Sidebar navigation for semantic threads.
- Interface persona switching module.
- Memory graph visual viewer.

---

## 7. ENVIRONMENT, VISUAL DESIGN & UI/UX SPECIFICATIONS

### 7.1 Typewriter-Minimal Interface Aesthetics

- **Visual Engine & Palette:** High-contrast, operator-grade interface rendered
  in a strict black and off-white monochrome palette.
- **Geometry & Layout:** Zero rounded corners (0px border-radius), crisp spatial
  boundaries, and precision grid alignment.
- **Prohibited Visual Elements:** Absolute omission of message speech bubbles,
  emojis, gradients, decorative drop shadows, or consumer UI embellishments.
- **Core Focus:** High-density, clean typography, semantic markers, and
  mechanical precision.

### 7.2 Environmental Depth & Interactive Inspection Infrastructure

- **Tactile System Canvas:** Sleek, ultra-responsive environment that embeds
  system telemetry, state inspection, and execution depth directly into the UI
  substrate without requiring conversational follow-up queries.
- **Sub-Surface Drill-Down:** Direct interaction (hover/click) on interface
  objects reveals inline split-blade inspection panels, exposing vector
  distances, confidence weightings, token-level probabilities, and model
  generation parameters.
- **Visual Real-Time Telemetry:** In-environment exposure of live execution
  stacks, memory-graph citations, intent parser metrics, and active state
  variables embedded directly into the workspace canvas.

### 7.3 Dynamic Message States & Visual Markers

Messages must visually shift their layout and presentation dynamically based on
message type, status, and classification, providing absolute clarity without UI
clutter:

- **Intent Color Band:** High-contrast edge bar visually designating classified
  communication intent.
- **Persona Tag:** Explicit indicator displaying the active persona filter
  applied to the message.
- **Confidence Indicator:** Real-time visual density/numerical bar displaying
  precision and verification certainty.
- **Thread Affiliation:** Direct, actionable marker linking the message to its
  primary and secondary semantic threads.
- **Execution Marker:** Visual status indicator confirming command execution,
  pending background tasks, or active tool output.

### 7.4 Non-Modal Inline Utilities

Direct hover or tap interactions on any message object reveal an unobtrusive
utility bar directly in-line, operating completely free of pop-ups, modal
overlay windows, or page transitions:

- **Transform:** Trigger immediate inline structural mutations.
- **Summarize:** Compress content directly within the active message stream.
- **Expand:** Unfold hidden contextual details or technical breakdowns.
- **Thread Link:** Navigate directly to associated semantic thread paths.
- **Memory Link:** Reveal exact memory-graph nodes utilized to construct the
  response.
- **Persona Switch:** Re-render the specific message using an alternate persona
  filter.

---

## Engineering Mapping

| Spec section | System module | Build track (LOGOS.md) |
|---|---|---|
| 2.1 Intent Parsing Engine | Intent Parser | T3 (cognition) |
| 2.2 Adaptive Persona Engine | Persona Engine | T3 (cognition) |
| 2.3 Semantic Threading Engine | Semantic Thread Manager | T3 (cognition) |
| 3.1 Inline Transformations | Message Transformer | T3 (cognition) + T4 (integration) |
| 3.2 Native Conversational Tools | Inline Utility Toolkit | T3 (cognition) |
| 3.3 Conversational Memory Graph | Memory Graph | T3 (cognition, phased) |
| 6.2 Message Object Schema | Data layer | T2 (API) |
| 4 / 7 UI + Environment | SPA rendering | T1 (reskin) → T4 → T5 |
| 5 Operational behavior | Adaptive Response Generator | T3 → T4 |

---

## 8. UPGRADED APP — AESTHETIC INFRASTRUCTURE & VISUAL LANGUAGE (2026-08-07)

The app is NOT a reskinned chat log. It is a re-architected multi-pane
operator console. This section is the authoritative chassis spec.

### 8.1 Monochromatic Precision Palette
- Pure black background `#000000`; off-white typography `#F4F4F0`; structural
  charcoal borders `#1A1A1A`.
- Accent color is STRICTLY functional, restricted to 2px semantic indicators:
  Phosphor Green (verified commands), High-Contrast Amber (intent bounds),
  Cobalt Cyan (thread routing).
- Zero decorative artifacts: no gradients, drop shadows, glassmorphism,
  background blurs, or ambient glow. Every element maps to computational state.

### 8.2 Structural Geometry & Typography
- Hard geometry: 0px border radius everywhere; panels tile along a rigid grid,
  no floating overlaps.
- Dual-font system:
  - Body/dialogue: dense high-legibility neutral sans (Inter / Neue Haas Grotesk)
  - Telemetry/system data: fixed-width mono (JetBrains Mono / SF Mono) for
    metadata, intent tags, confidence scores, code blocks.

### 8.3 Interactive Spatial Mechanics & Layout
- Fixed tiled grid: the screen is a multi-pane matrix — message streams,
  thread managers, telemetry blades sit flush against one another (not a
  scrolling web document).
- Vertical semantic margin: dedicated left-hand utility margin along the
  conversation block holding static 1px alignment grid marks, line counters,
  and intent indicators.
- Contextual split-blades: no modal overlays/pop-ups — the chassis physically
  shifts, sliding adjacent panels laterally on a single rendering plane to
  expose sub-surface telemetry.

### 8.4 Message Object Anatomy & Visual States
Layout per message:
```
+------------------------------------------------------------------+
| [ASK] [ANALYST] [CONF: 0.96] [THREAD: #ARCH-01] [EXEC: COMPLETED / 12ms] |
+------------------------------------------------------------------+
| Core message content in dense off-white typography, 1px grid boundaries. |
| Hover/focus reveals inline transformation controls beneath.       |
+------------------------------------------------------------------+
| > [TRANSFORM] [CONDENSE] [EXPAND] [THREAD-LINK] [MEMORY-LINK] [SWITCH-PERSONA] |
+------------------------------------------------------------------+
```
- Intent edge band: sharp 2px vertical bar at far-left margin of each message.
  Colors: Ask = Neutral Light Gray #D1D1D1; Command = High-Contrast Amber
  #FFB000; Reflect = Deep Slate #4A5568; Draft = Phosphor Green #00FF66;
  Clarify = Signal Magenta #FF0055; Explore = Cobalt Blue #0088FF.
- Monospaced metadata header above every message body:
  `[INTENT: COMMAND] [PERSONA: OPERATOR] [CONFIDENCE: 98.4%] [THREAD_ID: #SYS-LOG-042] [LATENCY: 14ms]`
- Non-modal inline action rail: flush to bottom edge of message frame on
  hover/select. Text-trigger monospace blocks ([XFORM] [CONDENSE] [EXPAND]
  [MEM-NODE] [REWRITE]). Clicking triggers in-place transformation animation —
  the original block morphs/expands without changing screen focal point.

### 8.5 Deep-Inspection Mechanics (the "underneath")
Environment exposes the intelligence layer through tactile interaction, not
conversational prompts:
- Inline inspector blade slides out on hover/click of telemetry tags:
  token latency, total tokens, intent vector score, memory citations
  (#NODE-881, #NODE-304), token logprobs ("Deploy" 0.99, ...).
- Modifier-key inspection mode (Alt/Option): viewport switches to Diagnostic
  Telemetry View — text opacity drops to 60%, 1px dashed vector association
  lines draw across the canvas connecting related messages to parent threads
  and memory nodes, token generation probabilities render as tiny percentages
  above key terms.
- In-canvas split-blade: clicking any `[CONF: 0.XX]` tag opens a side-by-side
  inspection panel inside the grid: memory citations, raw JSON payload dump of
  the Message Data Object, execution stack (intent parser + persona engine
  steps).

### 8.6 Motion Dynamics & Tactile Ergonomics
- Micro-transitions: zero easing/bounciness; ultra-fast linear or step-function
  timing (100-150ms); rigid mechanical precision.
- Layout shifts: panels expand/split-blades open with adjacent blocks instantly
  adjusting width along grid lines — physical chassis hardware sliding into
  locked positions.
- Keyboard-first:
  - Cmd+K or `/` opens inline monospaced command bar spanning the active input
    frame width.
  - Vim/terminal-style navigation: navigate message objects, initiate
    transformations, switch personas (`:persona operator`), drill into
    telemetry blades (`:inspect`) — full hotkey operation, no mouse required.
