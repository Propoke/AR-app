// Schema-conformance test (Phase 4).
//
// Validates representative AnnotationEvent and SignalingEnvelope payloads — the
// shapes the Windows agent and Unity client serialize — against the shared JSON
// Schemas in shared/. This keeps all three implementations honest about the wire
// contract: if a client adds/renames a field, a sample here should be updated and
// the schema kept in sync, or this test fails.

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";

const here = dirname(fileURLToPath(import.meta.url));
const sharedDir = join(here, "..", "..", "shared");

const load = (name) => JSON.parse(readFileSync(join(sharedDir, name), "utf8"));
const annotationSchema = load("annotation.schema.json");
const signalingSchema = load("signaling.schema.json");

const ajv = new Ajv2020({ allErrors: true, strict: false });
const validateAnnotation = ajv.compile(annotationSchema);
const validateSignaling = ajv.compile(signalingSchema);

let failures = 0;
function check(label, validate, sample, shouldPass) {
  const ok = validate(sample);
  if (ok !== shouldPass) {
    failures++;
    console.error(`✗ ${label}: expected ${shouldPass ? "valid" : "invalid"}, got ${ok ? "valid" : "invalid"}`);
    if (validate.errors) console.error("  ", ajv.errorsText(validate.errors));
  } else {
    console.log(`✓ ${label}`);
  }
}

// --- AnnotationEvent: valid samples (what the clients emit) ---
check("annotation create arrow", validateAnnotation, {
  op: "create", id: "a1", kind: "arrow", point: { u: 0.5, v: 0.42 }, color: "#ff3b30",
}, true);

check("annotation freehand path", validateAnnotation, {
  op: "create", id: "a2", kind: "freehand",
  path: [{ u: 0.1, v: 0.1 }, { u: 0.2, v: 0.2 }], color: "#00aaff",
}, true);

check("annotation phone echo with anchorId", validateAnnotation, {
  op: "update", id: "a1", anchorId: "trackable-123",
}, true);

check("annotation clear", validateAnnotation, { op: "clear", id: "a3" }, true);

// --- AnnotationEvent: invalid samples (must be rejected) ---
check("annotation bad op", validateAnnotation, { op: "nope", id: "x" }, false);
check("annotation out-of-range point", validateAnnotation,
  { op: "create", id: "x", kind: "arrow", point: { u: 1.5, v: 0 } }, false);
check("annotation bad color", validateAnnotation,
  { op: "create", id: "x", kind: "marker", point: { u: 0, v: 0 }, color: "red" }, false);
check("annotation missing id", validateAnnotation, { op: "create" }, false);

// --- SignalingEnvelope ---
check("signaling offer", validateSignaling,
  { type: "offer", from: "agent", payload: { sdp: "v=0..." } }, true);
check("signaling annotation", validateSignaling,
  { type: "annotation", from: "agent", payload: { op: "create", id: "a1" } }, true);
check("signaling bad type", validateSignaling, { type: "frobnicate" }, false);
check("signaling bad role", validateSignaling, { type: "ice", from: "server" }, false);

if (failures > 0) {
  console.error(`\n[schema] FAIL — ${failures} case(s) did not match the shared schemas`);
  process.exit(1);
}
console.log("\n[schema] PASS — sample payloads conform to shared/*.json");
