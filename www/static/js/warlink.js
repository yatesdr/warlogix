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
        var placeholder = opts.placeholder || 'Search tags...';
        var onSelect = opts.onSelect || function() {};
        var allTags = [];
        var filteredTags = [];
        var highlightIndex = -1;
        var isOpen = false;
        var maxVisible = 200;

        // Build DOM
        var wrapper = document.createElement('div');
        wrapper.className = 'tag-picker';

        var input = document.createElement('input');
        input.type = 'text';
        input.className = 'tag-picker-input';
        input.placeholder = placeholder;
        if (currentValue) input.value = currentValue;

        var dropdown = document.createElement('div');
        dropdown.className = 'tag-picker-dropdown';
        dropdown.style.display = 'none';

        var list = document.createElement('div');
        list.className = 'tag-picker-list';
        dropdown.appendChild(list);

        wrapper.appendChild(input);
        wrapper.appendChild(dropdown);
        containerEl.innerHTML = '';
        containerEl.appendChild(wrapper);

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
            api.get('/htmx/plc-tags/' + encodeURIComponent(currentPLC)).then(function(tags) {
                allTags = tags || [];
                tagPickerCache[currentPLC] = allTags;
                render();
            }).catch(function() {
                allTags = [];
                render();
            });
        }

        function render() {
            var query = input.value.toLowerCase();
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

            if (filteredTags.length === 0) {
                var empty = document.createElement('div');
                empty.className = 'tag-picker-empty';
                empty.textContent = currentPLC ? 'No matching tags' : 'Select a PLC first';
                list.appendChild(empty);
            } else {
                if (filteredTags.length > maxVisible) {
                    var hint = document.createElement('div');
                    hint.className = 'tag-picker-empty';
                    hint.textContent = 'Showing ' + maxVisible + ' of ' + filteredTags.length + ' — type to filter...';
                    list.appendChild(hint);
                }
                for (var j = 0; j < limit; j++) {
                    var item = document.createElement('div');
                    item.className = 'tag-picker-item';
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
            }

            if (highlightIndex >= limit) highlightIndex = limit - 1;
        }

        function open() {
            if (isOpen) return;
            isOpen = true;
            dropdown.style.display = '';
            highlightIndex = -1;
            render();
        }

        function close() {
            if (!isOpen) return;
            isOpen = false;
            dropdown.style.display = 'none';
        }

        function selectIndex(idx) {
            if (idx < 0 || idx >= filteredTags.length) return;
            var tag = filteredTags[idx];
            currentValue = tag.name;
            input.value = tag.name;
            close();
            onSelect(tag.name, tag.type);
        }

        // Event: input focus
        input.addEventListener('focus', function() {
            open();
        });

        // Event: input typing
        input.addEventListener('input', function() {
            highlightIndex = -1;
            if (!isOpen) open();
            render();
        });

        // Event: keyboard navigation
        input.addEventListener('keydown', function(e) {
            if (!isOpen) {
                if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                    open();
                    e.preventDefault();
                }
                return;
            }
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
                input.value = currentValue;
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
                // Restore current value if input was changed but not selected
                if (input.value !== currentValue) {
                    input.value = currentValue;
                }
            }
        }
        document.addEventListener('mousedown', onDocClick);

        // Initial fetch
        if (currentPLC) fetchTags();

        return {
            setPLC: function(plcName) {
                if (plcName === currentPLC) return;
                currentPLC = plcName;
                currentValue = '';
                input.value = '';
                fetchTags();
            },
            setValue: function(val) {
                currentValue = val || '';
                input.value = currentValue;
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
        TagPicker: TagPicker
    };
})();
