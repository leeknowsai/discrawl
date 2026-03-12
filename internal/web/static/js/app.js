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

// SSE reconnection with backoff
let sseRetryCount = 0;
const maxRetries = 10;
const baseDelay = 1000; // 1 second

document.addEventListener('htmx:sseError', function (e) {
    console.log('SSE connection error, will auto-reconnect');
    sseRetryCount++;
    if (sseRetryCount >= maxRetries) {
        console.warn('SSE max retries reached, stopping reconnection attempts');
        return;
    }
    // Exponential backoff: 1s, 2s, 4s, 8s, max 30s
    const delay = Math.min(baseDelay * Math.pow(2, sseRetryCount - 1), 30000);
    console.log(`SSE reconnecting in ${delay}ms (attempt ${sseRetryCount})`);
});

document.addEventListener('htmx:sseOpen', function () {
    console.log('SSE connection established');
    sseRetryCount = 0; // Reset retry counter on successful connection
});

document.addEventListener('htmx:sseClose', function () {
    console.log('SSE connection closed');
});

// Keyword alert highlighting
let keywordAlerts = [];

// Load keyword alerts from API on page load
document.addEventListener('DOMContentLoaded', function () {
    loadKeywordAlerts();
});

function loadKeywordAlerts() {
    fetch('/api/alerts')
        .then(response => {
            if (!response.ok) throw new Error('Failed to load alerts');
            return response.json();
        })
        .then(alerts => {
            keywordAlerts = alerts || [];
            console.log(`Loaded ${keywordAlerts.length} keyword alerts`);
            // Highlight existing messages on page
            highlightAllMessages();
        })
        .catch(err => {
            console.error('Error loading keyword alerts:', err);
        });
}

// Highlight all visible messages based on keyword alerts
function highlightAllMessages() {
    if (keywordAlerts.length === 0) return;

    const messages = document.querySelectorAll('[data-message-id]');
    messages.forEach(msg => {
        const content = msg.textContent || '';
        if (matchesAnyKeyword(content)) {
            msg.classList.add('keyword-highlighted');
        }
    });
}

// Check if content matches any keyword alert
function matchesAnyKeyword(content) {
    if (!content || keywordAlerts.length === 0) return false;

    const lowerContent = content.toLowerCase();
    return keywordAlerts.some(alert => {
        if (!alert.keyword) return false;
        const keyword = alert.keyword.toLowerCase();
        return lowerContent.includes(keyword);
    });
}

// Highlight new messages arriving via SSE
document.addEventListener('htmx:afterSwap', function (e) {
    // Check if this was an SSE event (message update)
    const target = e.detail.target;
    if (target && keywordAlerts.length > 0) {
        // If the swapped element is a message, check for keyword matches
        if (target.hasAttribute('data-message-id')) {
            const content = target.textContent || '';
            if (matchesAnyKeyword(content)) {
                target.classList.add('keyword-highlighted');
            }
        } else {
            // If the swap updated a container, check all messages inside
            const messages = target.querySelectorAll('[data-message-id]');
            messages.forEach(msg => {
                const content = msg.textContent || '';
                if (matchesAnyKeyword(content)) {
                    msg.classList.add('keyword-highlighted');
                }
            });
        }
    }
});
