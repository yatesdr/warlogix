// WarLink - Shared JavaScript utilities
var WarLink = (function() {
    'use strict';

    // --- Utility Functions ---

    function escapeHtml(text) {
        if (text === null || text === undefined) return '';
        var div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    function escapeSelector(str) {
        return str.replace(/([!"#$%&'()*+,.\/:;<=>?@[\\\]^`{|}~])/g, '\\$1');
    }

    function capitalizeFirst(str) {
        if (!str) return '';
        return str.charAt(0).toUpperCase() + str.slice(1);
    }

    // --- SSE Factory ---

    function createSSE(url, handlers) {
        var es = null;
        var reconnectTimeout = null;
        var connected = false;

        function connect() {
            if (es) {
                es.close();
            }

            es = new EventSource(url || '/events/republisher');

            es.addEventListener('connected', function(e) {
                connected = true;
                if (handlers.onConnected) {
                    handlers.onConnected(JSON.parse(e.data));
                }
            });

            // Register custom event handlers
            Object.keys(handlers).forEach(function(key) {
                if (key === 'onConnected' || key === 'onError') return;
                // Convert camelCase handler name to event-type
                // e.g. onValueChange -> value-change, onMqttStatus -> mqtt-status
                var eventType = key.replace(/^on/, '').replace(/([A-Z])/g, function(m) {
                    return '-' + m.toLowerCase();
                }).replace(/^-/, '');

                es.addEventListener(eventType, function(e) {
                    try {
                        var data = JSON.parse(e.data);
                        handlers[key](data);
                    } catch (err) {
                        console.error('SSE ' + eventType + ' parse error:', err);
                    }
                });
            });

            es.onerror = function() {
                connected = false;
                es.close();
                if (handlers.onError) {
                    handlers.onError();
                }
                if (reconnectTimeout) clearTimeout(reconnectTimeout);
                reconnectTimeout = setTimeout(connect, 1000);
            };
        }

        function disconnect() {
            if (reconnectTimeout) clearTimeout(reconnectTimeout);
            if (es) {
                es.close();
                es = null;
            }
            connected = false;
        }

        // Auto-connect
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', connect);
        } else {
            connect();
        }

        return {
            connect: connect,
            disconnect: disconnect,
            isConnected: function() { return connected; }
        };
    }

    // --- Modal Helpers ---

    function showModal(id) {
        var modal = document.getElementById(id);
        if (modal) modal.style.display = 'flex';
    }

    function hideModal(id) {
        var modal = document.getElementById(id);
        if (modal) modal.style.display = 'none';
    }

    // --- Fetch API Wrappers ---

    var api = {
        get: function(url) {
            return fetch(url).then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(msg) { throw new Error(msg); });
                }
                return resp.json();
            });
        },

        post: function(url, data) {
            return fetch(url, {
                method: 'POST',
                headers: data ? {'Content-Type': 'application/json'} : {},
                body: data ? JSON.stringify(data) : undefined
            }).then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(msg) { throw new Error(msg); });
                }
                // Return JSON if content exists, otherwise null
                var ct = resp.headers.get('content-type');
                if (ct && ct.indexOf('application/json') >= 0) {
                    return resp.json();
                }
                return null;
            });
        },

        put: function(url, data) {
            return fetch(url, {
                method: 'PUT',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(data)
            }).then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(msg) { throw new Error(msg); });
                }
                var ct = resp.headers.get('content-type');
                if (ct && ct.indexOf('application/json') >= 0) {
                    return resp.json();
                }
                return null;
            });
        },

        del: function(url) {
            return fetch(url, { method: 'DELETE' }).then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(msg) { throw new Error(msg); });
                }
                return null;
            });
        },

        patch: function(url, data) {
            return fetch(url, {
                method: 'PATCH',
                headers: data ? {'Content-Type': 'application/json'} : {},
                body: data ? JSON.stringify(data) : undefined
            }).then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(msg) { throw new Error(msg); });
                }
                var ct = resp.headers.get('content-type');
                if (ct && ct.indexOf('application/json') >= 0) {
                    return resp.json();
                }
                return null;
            });
        }
    };

    // --- Toast Notifications ---

    var toastContainer = null;

    function ensureToastContainer() {
        if (!toastContainer) {
            toastContainer = document.createElement('div');
            toastContainer.className = 'toast-container';
            document.body.appendChild(toastContainer);
        }
    }

    function toast(message, type) {
        ensureToastContainer();
        type = type || 'info';
        var el = document.createElement('div');
        el.className = 'toast toast-' + type;
        el.textContent = message;
        toastContainer.appendChild(el);
        // Trigger animation
        requestAnimationFrame(function() {
            el.classList.add('toast-show');
        });
        // Auto-remove after 3s
        setTimeout(function() {
            el.classList.remove('toast-show');
            setTimeout(function() {
                if (el.parentNode) el.parentNode.removeChild(el);
            }, 300);
        }, 3000);
    }

    // --- Tag Picker Component ---

    var tagPickerCache = {};

    function TagPicker(containerEl, opts) {
        opts = opts || {};
        var currentPLC = opts.plc || '';
        var currentValue = opts.value || '';
        var allowEmpty = opts.allowEmpty || false;
        var placeholder = opts.placeholder || 'Select a tag...';
        var onSelect = opts.onSelect || function() {};
        var allTags = [];
        var filteredTags = [];
        var highlightIndex = -1;
        var isOpen = false;
        var maxVisible = 200;
        var loading = false;

        // Build DOM
        var wrapper = document.createElement('div');
        wrapper.className = 'tag-picker';

        var display = document.createElement('div');
        display.className = 'tag-picker-display';
        display.tabIndex = 0;

        var displayText = document.createElement('span');
        displayText.className = 'tag-picker-display-text';
        displayText.textContent = currentValue || placeholder;
        if (!currentValue) displayText.classList.add('placeholder');

        var arrow = document.createElement('span');
        arrow.className = 'tag-picker-arrow';
        arrow.innerHTML = '&#9662;';

        display.appendChild(displayText);
        display.appendChild(arrow);

        var dropdown = document.createElement('div');
        dropdown.className = 'tag-picker-dropdown';
        dropdown.style.display = 'none';

        var filterInput = document.createElement('input');
        filterInput.type = 'text';
        filterInput.className = 'tag-picker-filter';
        filterInput.placeholder = 'Type to filter...';

        var list = document.createElement('div');
        list.className = 'tag-picker-list';

        var status = document.createElement('div');
        status.className = 'tag-picker-status';

        dropdown.appendChild(filterInput);
        dropdown.appendChild(list);
        dropdown.appendChild(status);

        wrapper.appendChild(display);
        wrapper.appendChild(dropdown);
        containerEl.innerHTML = '';
        containerEl.appendChild(wrapper);

        function updateDisplay() {
            if (currentValue) {
                displayText.textContent = currentValue;
                displayText.classList.remove('placeholder');
            } else {
                displayText.textContent = placeholder;
                displayText.classList.add('placeholder');
            }
        }

        function updateStatus() {
            if (loading) {
                status.textContent = 'Loading tags...';
                status.style.display = '';
            } else if (!currentPLC) {
                status.textContent = 'Select a PLC first';
                status.style.display = '';
            } else if (allTags.length === 0) {
                status.textContent = 'No tags available';
                status.style.display = '';
            } else if (filteredTags.length === 0) {
                status.textContent = 'No matching tags';
                status.style.display = '';
            } else if (filteredTags.length > maxVisible) {
                status.textContent = 'Showing ' + maxVisible + ' of ' + filteredTags.length + ' tags';
                status.style.display = '';
            } else {
                status.textContent = filteredTags.length + ' tag' + (filteredTags.length !== 1 ? 's' : '');
                status.style.display = '';
            }
        }

        function fetchTags() {
            if (!currentPLC) {
                allTags = [];
                render();
                return;
            }
            if (tagPickerCache[currentPLC]) {
                allTags = tagPickerCache[currentPLC];
                render();
                return;
            }
            loading = true;
            render();
            api.get('/htmx/plc-tags/' + encodeURIComponent(currentPLC)).then(function(tags) {
                allTags = tags || [];
                tagPickerCache[currentPLC] = allTags;
                loading = false;
                render();
            }).catch(function() {
                allTags = [];
                loading = false;
                render();
            });
        }

        function render() {
            var query = filterInput.value.toLowerCase();
            filteredTags = [];

            if (allowEmpty) {
                filteredTags.push({name: '', type: '(none)'});
            }

            for (var i = 0; i < allTags.length; i++) {
                var t = allTags[i];
                if (!query || t.name.toLowerCase().indexOf(query) >= 0 || t.type.toLowerCase().indexOf(query) >= 0) {
                    filteredTags.push(t);
                }
            }

            list.innerHTML = '';
            var limit = Math.min(filteredTags.length, maxVisible);

            for (var j = 0; j < limit; j++) {
                var item = document.createElement('div');
                item.className = 'tag-picker-item';
                if (filteredTags[j].name === currentValue) item.classList.add('current');
                if (j === highlightIndex) item.classList.add('highlighted');
                item.dataset.index = j;

                var nameSpan = document.createElement('span');
                nameSpan.className = 'tag-picker-item-name';
                nameSpan.textContent = filteredTags[j].name || '(none)';

                var typeSpan = document.createElement('span');
                typeSpan.className = 'tag-picker-item-type';
                typeSpan.textContent = filteredTags[j].type;

                item.appendChild(nameSpan);
                item.appendChild(typeSpan);
                list.appendChild(item);
            }

            if (highlightIndex >= limit) highlightIndex = limit - 1;
            updateStatus();
        }

        function positionDropdown() {
            // Reset to default (below)
            wrapper.classList.remove('dropup');
            var rect = display.getBoundingClientRect();
            var spaceBelow = window.innerHeight - rect.bottom;
            var dropdownHeight = Math.min(dropdown.scrollHeight, 320);
            if (spaceBelow < dropdownHeight && rect.top > spaceBelow) {
                wrapper.classList.add('dropup');
            }
        }

        function open() {
            if (isOpen) return;
            isOpen = true;
            wrapper.classList.add('open');
            dropdown.style.display = '';
            filterInput.value = '';
            highlightIndex = -1;
            render();
            positionDropdown();
            filterInput.focus();
            // Scroll current value into view
            var cur = list.querySelector('.tag-picker-item.current');
            if (cur) cur.scrollIntoView({block: 'nearest'});
        }

        function close() {
            if (!isOpen) return;
            isOpen = false;
            wrapper.classList.remove('open');
            wrapper.classList.remove('dropup');
            dropdown.style.display = 'none';
        }

        function selectIndex(idx) {
            if (idx < 0 || idx >= filteredTags.length) return;
            var tag = filteredTags[idx];
            currentValue = tag.name;
            updateDisplay();
            close();
            onSelect(tag.name, tag.type);
        }

        // Event: click display to toggle
        display.addEventListener('click', function() {
            if (isOpen) { close(); } else { open(); }
        });
        display.addEventListener('keydown', function(e) {
            if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
                e.preventDefault();
                if (!isOpen) open();
            }
        });

        // Event: filter typing
        filterInput.addEventListener('input', function() {
            highlightIndex = -1;
            render();
        });

        // Event: keyboard navigation in filter
        filterInput.addEventListener('keydown', function(e) {
            var limit = Math.min(filteredTags.length, maxVisible);
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                highlightIndex = Math.min(highlightIndex + 1, limit - 1);
                render();
                scrollToHighlight();
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                highlightIndex = Math.max(highlightIndex - 1, 0);
                render();
                scrollToHighlight();
            } else if (e.key === 'Enter') {
                e.preventDefault();
                if (highlightIndex >= 0) {
                    selectIndex(highlightIndex);
                }
            } else if (e.key === 'Escape') {
                close();
                display.focus();
            }
        });

        function scrollToHighlight() {
            var el = list.querySelector('.tag-picker-item.highlighted');
            if (el) el.scrollIntoView({block: 'nearest'});
        }

        // Event: click on item
        list.addEventListener('click', function(e) {
            var item = e.target.closest('.tag-picker-item');
            if (item) {
                selectIndex(parseInt(item.dataset.index, 10));
            }
        });

        // Event: click outside
        function onDocClick(e) {
            if (!wrapper.contains(e.target)) {
                close();
            }
        }
        document.addEventListener('mousedown', onDocClick);

        // Initial fetch
        if (currentPLC) fetchTags();
        updateDisplay();

        return {
            setPLC: function(plcName) {
                if (plcName === currentPLC) return;
                currentPLC = plcName;
                currentValue = '';
                updateDisplay();
                fetchTags();
            },
            setValue: function(val) {
                currentValue = val || '';
                updateDisplay();
            },
            getValue: function() {
                return currentValue;
            },
            destroy: function() {
                document.removeEventListener('mousedown', onDocClick);
                containerEl.innerHTML = '';
            }
        };
    }

    // --- Theme Management ---

    // Stored preference: 'light', 'dark', or null (system)
    function getStoredTheme() {
        return localStorage.getItem('theme');
    }

    // Resolve to actual 'light' or 'dark'
    function getEffectiveTheme() {
        var stored = getStoredTheme();
        if (stored === 'light' || stored === 'dark') return stored;
        return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }

    function applyTheme() {
        var effective = getEffectiveTheme();
        document.documentElement.dataset.theme = effective;
        var btn = document.querySelector('.theme-toggle');
        if (!btn) return;
        var stored = getStoredTheme();
        if (stored === 'dark') {
            btn.textContent = '\u263D'; // moon
            btn.title = 'Theme: dark (click for system)';
        } else if (!stored) {
            btn.textContent = '\u25D0'; // half circle
            btn.title = 'Theme: system (click for light)';
        } else {
            btn.textContent = '\u2600'; // sun
            btn.title = 'Theme: light (click for dark)';
        }
    }

    // Cycle: light -> dark -> system -> light
    function toggleTheme() {
        var stored = getStoredTheme();
        if (stored === 'light') {
            localStorage.setItem('theme', 'dark');
        } else if (stored === 'dark') {
            localStorage.removeItem('theme');
        } else {
            localStorage.setItem('theme', 'light');
        }
        applyTheme();
    }

    function initTheme() {
        applyTheme();
        window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function() {
            if (!getStoredTheme()) applyTheme();
        });
    }

    // Auto-init
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initTheme);
    } else {
        initTheme();
    }

    // --- Public API ---

    return {
        escapeHtml: escapeHtml,
        escapeSelector: escapeSelector,
        capitalizeFirst: capitalizeFirst,
        createSSE: createSSE,
        showModal: showModal,
        hideModal: hideModal,
        api: api,
        toast: toast,
        TagPicker: TagPicker,
        toggleTheme: toggleTheme
    };
})();
