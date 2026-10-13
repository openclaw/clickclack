import test from "node:test";
import assert from "node:assert/strict";
import { pushLandingURL, takePushLanding } from "./push-landing.ts";

test("a tap that opens a window marks the path, and the app takes the mark back off", () => {
  const marked = pushLandingURL("/app/wsp_1/chn_1");
  assert.equal(marked, "/app/wsp_1/chn_1?from=push");
  assert.equal(takePushLanding(`https://chat.example.com${marked}`), "/app/wsp_1/chn_1");
});

test("the mark keeps whatever else the path carries", () => {
  const marked = pushLandingURL("/app/wsp_1/dm_1?topic=x#end");
  assert.equal(marked, "/app/wsp_1/dm_1?topic=x&from=push#end");
  assert.equal(takePushLanding(`https://chat.example.com${marked}`), "/app/wsp_1/dm_1?topic=x#end");
});

test("a page no tap opened carries no mark", () => {
  assert.equal(takePushLanding("https://chat.example.com/app/wsp_1/chn_1"), null);
  assert.equal(takePushLanding("https://chat.example.com/app/wsp_1/chn_1?from=link"), null);
});
