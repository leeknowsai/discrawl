// discrawl app.js – minimal HTMX config and event listeners

// HTMX global config
if (typeof htmx !== 'undefined') {
    htmx.config.defaultSwapStyle = 'innerHTML';
    htmx.config.historyCacheSize = 0;
    htmx.config.refreshOnHistoryMiss = true;
}

// Chart refresh event listener placeholder
document.addEventListener('discrawl:refresh-charts', function (e) {
    // Phase 4: trigger chart data reload
    const detail = e.detail || {};
    if (detail.target) {
        const el = document.getElementById(detail.target);
        if (el && typeof htmx !== 'undefined') {
            htmx.trigger(el, 'refresh');
        }
    }
});

// SSE reconnect helper placeholder
document.addEventListener('DOMContentLoaded', function () {
    // Phase 5: SSE live update setup goes here
});
