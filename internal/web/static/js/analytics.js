async function loadChart(canvasId, endpoint, chartType, options) {
    options = options || {};
    var canvas = document.getElementById(canvasId);
    if (!canvas) return;
    try {
        var resp = await fetch(endpoint);
        if (!resp.ok) return;
        var data = await resp.json();
        if (window._charts && window._charts[canvasId]) {
            window._charts[canvasId].data = data;
            window._charts[canvasId].update();
            return;
        }
        window._charts = window._charts || {};
        window._charts[canvasId] = new Chart(canvas, { type: chartType, data: data, options: options });
    } catch (e) {
        console.error('loadChart error', canvasId, e);
    }
}

function refreshCharts() {
    var days = (document.getElementById('days-filter') || {}).value || 30;
    var guildID = (document.getElementById('guild-id') || {}).value;
    if (!guildID) return;
    var base = '/api/v1/g/' + guildID + '/stats';
    loadChart('msg-volume', base + '/message-volume?days=' + days, 'bar', { responsive: true, plugins: { legend: { display: false } } });
    loadChart('top-members', base + '/top-members?days=' + days, 'bar', { indexAxis: 'y', responsive: true, plugins: { legend: { display: false } } });
    loadChart('channel-activity', base + '/channel-activity?days=' + days, 'bar', { indexAxis: 'y', responsive: true, plugins: { legend: { display: false } } });
}

document.addEventListener('DOMContentLoaded', refreshCharts);
document.addEventListener('chartRefresh', refreshCharts);
