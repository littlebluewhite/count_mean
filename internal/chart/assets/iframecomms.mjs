/* chart iframe-comms primitives (ADR-0003 family).

   Loaded into chart iframe customJS via //go:embed by assets.go; helpers
   exposed at window.__chartComms.{postToParent, isFromParent, handlePngRequest}
   for the inline customJS body (CCI + Composer) to consume. Pure helpers — all
   inputs (myChart, the event, the reply-type) are runtime args, which is what
   makes this file byte-identical for both engines.

   No frontend twin: unlike phasemarkers.mjs (mirrored in frontend/src and
   sync-tested), the parent side of this protocol is a different impl
   (frontend/src/charts/iframeBridge.mjs), so there is no .test.mjs sync test.

   IMPORTANT: block-comment-only (slash-star ... star-slash). The whole
   concatenated customJS is newline-stripped by go-echarts AddJSFuncStrs; a
   line comment would eat to the next newline and blank the chart (image #5). */
(function () {
  /* KEEP this exact var name wailsParentOrigins — audit/grep target + asserted
     by internal/chart/assets/iframecomms_test.go and CCI source-string tests. */
  var wailsParentOrigins = ["wails://wails", "http://wails.localhost", "https://wails.localhost"];
  function postToParent(msg) {
    for (var i = 0; i < wailsParentOrigins.length; i++) {
      try { window.parent.postMessage(msg, wailsParentOrigins[i]); } catch (e) {}
    }
    /* dev fallback: wails dev parent origin (http://localhost:34115, dynamic
       port) is not in the allowlist; post once with '*', the parent listener
       validates e.origin itself. */
    try { window.parent.postMessage(msg, '*'); } catch (e) {}
  }
  function isFromParent(e) {
    return e.source === window.parent || wailsParentOrigins.indexOf(e.origin) !== -1;
  }
  function handlePngRequest(myChart, e, resultType) {
    var id = e.data.requestId;
    try {
      var url = myChart.getDataURL({ type: 'png', pixelRatio: 2, backgroundColor: '#fff' });
      postToParent({ type: resultType, requestId: id, payload: { dataURL: url } });
    } catch (err) {
      postToParent({ type: resultType, requestId: id, error: String(err) });
    }
  }
  window.__chartComms = { postToParent: postToParent, isFromParent: isFromParent,
                          handlePngRequest: handlePngRequest };
})();
