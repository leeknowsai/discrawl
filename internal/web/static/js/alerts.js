// Keyword alerts client-side matching and highlighting
(function() {
    'use strict';

    // Cache for current user's alerts
    let userAlerts = [];
    let keywordPatterns = [];

    // Fetch user's keyword alerts for the current guild
    function loadAlerts() {
        const guildID = getGuildIDFromURL();
        if (!guildID) return;

        fetch(`/api/v1/g/${guildID}/alerts`, {
            credentials: 'include'
        })
        .then(res => res.ok ? res.json() : Promise.reject(res))
        .then(data => {
            userAlerts = data.alerts || [];
            keywordPatterns = compileKeywordPatterns(userAlerts);
            highlightExistingMessages();
        })
        .catch(err => console.error('Failed to load alerts:', err));
    }

    // Extract guild ID from current URL
    function getGuildIDFromURL() {
        const match = window.location.pathname.match(/\/g\/([^\/]+)/);
        return match ? match[1] : null;
    }

    // Compile keyword patterns for efficient matching
    function compileKeywordPatterns(alerts) {
        const patterns = [];
        alerts.forEach(alert => {
            if (!alert.keywords) return;
            // Split comma-separated keywords
            const keywords = alert.keywords.split(',').map(k => k.trim()).filter(k => k);
            keywords.forEach(keyword => {
                try {
                    // Case-insensitive regex for whole word matching
                    const pattern = new RegExp(`\\b${escapeRegExp(keyword)}\\b`, 'gi');
                    patterns.push({ keyword, pattern });
                } catch (e) {
                    console.warn('Invalid keyword pattern:', keyword, e);
                }
            });
        });
        return patterns;
    }

    // Escape special regex characters
    function escapeRegExp(str) {
        return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }

    // Check if message content matches any keywords
    function matchesKeywords(content) {
        if (!content || keywordPatterns.length === 0) return false;
        return keywordPatterns.some(({ pattern }) => pattern.test(content));
    }

    // Highlight a single message element if it matches keywords
    function highlightMessage(messageEl) {
        const contentEl = messageEl.querySelector('[data-message-content]');
        if (!contentEl) return;

        const content = contentEl.textContent || contentEl.innerText;
        if (matchesKeywords(content)) {
            messageEl.classList.add('alert-keyword-match');
        }
    }

    // Highlight all existing messages on page
    function highlightExistingMessages() {
        const messages = document.querySelectorAll('[data-message-id]');
        messages.forEach(highlightMessage);
    }

    // Observer for new messages added via SSE
    function observeNewMessages() {
        const messageContainer = document.getElementById('message-list');
        if (!messageContainer) return;

        const observer = new MutationObserver(mutations => {
            mutations.forEach(mutation => {
                mutation.addedNodes.forEach(node => {
                    if (node.nodeType === Node.ELEMENT_NODE) {
                        if (node.hasAttribute('data-message-id')) {
                            highlightMessage(node);
                        } else {
                            const messages = node.querySelectorAll('[data-message-id]');
                            messages.forEach(highlightMessage);
                        }
                    }
                });
            });
        });

        observer.observe(messageContainer, { childList: true, subtree: true });
    }

    // Initialize on page load
    function init() {
        loadAlerts();
        observeNewMessages();
    }

    // Run on DOMContentLoaded or immediately if already loaded
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
