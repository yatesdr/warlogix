// RepublisherSSE - SSE client for real-time republisher updates
var RepublisherSSE = (function() {
    var es = null;
    var reconnectTimeout = null;
    var connected = false;

    function connect() {
        if (es) {
            es.close();
        }

        es = new EventSource('/events/republisher');

        es.addEventListener('connected', function(e) {
            connected = true;
            console.log('SSE connected:', JSON.parse(e.data).id);
        });

        es.addEventListener('value-change', function(e) {
            try {
                var data = JSON.parse(e.data);
                onValueChange(data);
            } catch (err) {
                console.error('SSE value-change parse error:', err);
            }
        });

        es.addEventListener('config-change', function(e) {
            try {
                var data = JSON.parse(e.data);
                onConfigChange(data);
            } catch (err) {
                console.error('SSE config-change parse error:', err);
            }
        });

        es.addEventListener('status-change', function(e) {
            try {
                var data = JSON.parse(e.data);
                onStatusChange(data);
            } catch (err) {
                console.error('SSE status-change parse error:', err);
            }
        });

        es.addEventListener('entity-change', function(e) {
            try {
                var data = JSON.parse(e.data);
                onEntityChange(data);
            } catch (err) {
                console.error('SSE entity-change parse error:', err);
            }
        });

        es.onerror = function() {
            connected = false;
            es.close();
            // Reconnect after 1 second
            if (reconnectTimeout) clearTimeout(reconnectTimeout);
            reconnectTimeout = setTimeout(connect, 1000);
        };
    }

    function onValueChange(data) {
        // Find the tag item in the DOM
        var plcGroup = document.querySelector('.plc-group[data-plc="' + WarLink.escapeSelector(data.plc) + '"]');
        if (!plcGroup) return;

        var tagItem = plcGroup.querySelector('.tag-item[data-name="' + WarLink.escapeSelector(data.tag) + '"]');
        if (!tagItem) return;

        // Update the stored JSON value
        var jsonValue = JSON.stringify(data.value);
        tagItem.dataset.json = jsonValue;

        // Update the type if provided
        if (data.type) {
            tagItem.dataset.type = data.type;
        }

        // Update the lastChanged timestamp if provided
        if (data.lastChanged) {
            tagItem.dataset.lastChanged = data.lastChanged;
        }

        // If value is a struct/object and the tag isn't yet expandable, upgrade it
        var isStruct = data.value !== null && typeof data.value === 'object' && !Array.isArray(data.value);
        if (isStruct && !tagItem.classList.contains('has-children')) {
            tagItem.classList.add('has-children', 'collapsed');
            tagItem.dataset.isStruct = 'true';
            tagItem.dataset.fieldCount = String(Object.keys(data.value).length);

            // Add expand arrow to the tag row
            var tagRow = tagItem.querySelector('.tag-row');
            if (tagRow && !tagRow.querySelector('.tag-expand')) {
                var expandSpan = document.createElement('span');
                expandSpan.className = 'tag-expand';
                expandSpan.innerHTML = '&#9660;';
                expandSpan.setAttribute('onclick', 'toggleTagExpand(event, this)');
                tagRow.insertBefore(expandSpan, tagRow.firstChild);
            }

            // Remove the inline value span (structs show field count in the tree display)
            var tagValue = tagRow ? tagRow.querySelector('.tag-value') : null;
            if (tagValue) tagValue.remove();

            // Add children container if missing
            if (!tagItem.querySelector(':scope > .tag-children')) {
                var childDiv = document.createElement('div');
                childDiv.className = 'tag-children';
                tagItem.appendChild(childDiv);
            }

            // Update tree display text to include field count
            var nameSpan = tagRow ? tagRow.querySelector('.tag-name') : null;
            if (nameSpan) {
                var fieldCount = Object.keys(data.value).length;
                var typeName = data.type || tagItem.dataset.type || '';
                var displayName = tagItem.dataset.name;
                // Use alias if set, or short display name
                var alias = tagItem.dataset.alias;
                if (alias) {
                    displayName = alias;
                } else {
                    // Extract short display name (last component after . or :)
                    var colonIdx = displayName.lastIndexOf(':');
                    if (colonIdx >= 0) displayName = displayName.substring(colonIdx + 1);
                    var dotIdx = displayName.lastIndexOf('.');
                    if (dotIdx >= 0) displayName = displayName.substring(dotIdx + 1);
                }
                var display = displayName;
                if (typeName) display += ' (' + typeName + ')';
                display += ' [' + fieldCount + ' fields]';
                nameSpan.textContent = display;
            }
        } else {
            // Update displayed value for non-struct types
            var tagValue = tagItem.querySelector('.tag-row .tag-value');
            if (tagValue) {
                tagValue.textContent = formatValue(data.value);
            }
        }

        // If this tag is currently selected, update the details panel
        if (typeof selectedInfo !== 'undefined' && selectedInfo &&
            selectedInfo.plc === data.plc && selectedInfo.tagName === data.tag) {
            selectedInfo.json = jsonValue;
            if (data.lastChanged) {
                selectedInfo.lastChanged = data.lastChanged;
            }
            if (typeof updateDetailsPanel === 'function') {
                updateDetailsPanel(selectedInfo);
            }
        }

        // Update children if expanded
        var childrenContainer = tagItem.querySelector(':scope > .tag-children');
        if (childrenContainer && childrenContainer.innerHTML !== '') {
            // Re-build children with new values while preserving expand state
            updateExpandedChildren(tagItem, data.value);
        }
    }

    function onConfigChange(data) {
        var plcGroup = document.querySelector('.plc-group[data-plc="' + WarLink.escapeSelector(data.plc) + '"]');
        if (!plcGroup) return;

        // Check if this is a child tag (contains a dot)
        var dotIdx = data.tag.indexOf('.');
        if (dotIdx > 0) {
            // This is a child tag - update the parent's publishedChildren
            var parentName = data.tag.substring(0, dotIdx);
            var childPath = data.tag.substring(dotIdx + 1);

            var tagItem = plcGroup.querySelector('.tag-item[data-name="' + WarLink.escapeSelector(parentName) + '"]');
            if (!tagItem) return;

            // Update publishedChildren data attribute
            var publishedChildren = {};
            try {
                publishedChildren = JSON.parse(tagItem.dataset.publishedChildren || '{}');
            } catch (e) {}

            if (data.enabled) {
                publishedChildren[childPath] = { enabled: data.enabled, writable: data.writable };
            } else {
                delete publishedChildren[childPath];
            }
            tagItem.dataset.publishedChildren = JSON.stringify(publishedChildren);

            // Update the child row's indicators if it exists
            var childRow = tagItem.querySelector('.tag-child-row[data-path="' + WarLink.escapeSelector(childPath) + '"]');
            if (childRow) {
                updateChildRowIndicators(childRow, data.enabled, data.writable, false);
                if (data.enabled) {
                    childRow.classList.add('published');
                } else {
                    childRow.classList.remove('published');
                }
            }

            // If this child is currently selected, update the details panel
            if (typeof selectedInfo !== 'undefined' && selectedInfo &&
                selectedInfo.plc === data.plc && selectedInfo.tagName === parentName && selectedInfo.path === childPath) {
                selectedInfo.memberPublished = data.enabled;
                selectedInfo.memberWritable = data.writable;
                if (typeof updateDetailsPanel === 'function') {
                    updateDetailsPanel(selectedInfo);
                }
            }
            return;
        }

        // This is a root tag
        var tagItem = plcGroup.querySelector('.tag-item[data-name="' + WarLink.escapeSelector(data.tag) + '"]');
        if (!tagItem) return;

        // Update data attributes
        tagItem.dataset.enabled = data.enabled;
        tagItem.dataset.writable = data.writable;
        tagItem.dataset.hasIgnores = data.ignores && data.ignores.length > 0;
        tagItem.dataset.ignoreCount = data.ignores ? data.ignores.length : 0;
        tagItem.dataset.ignoreList = JSON.stringify(data.ignores || []);

        // Update visual indicators
        var tagRow = tagItem.querySelector('.tag-row');
        if (tagRow) {
            if (data.enabled) {
                tagRow.classList.add('monitored');
            } else {
                tagRow.classList.remove('monitored');
            }
        }

        // Update indicator badges
        var indicators = tagItem.querySelector('.tag-indicators');
        if (indicators) {
            var html = '';
            if (data.enabled) {
                html += '<span class="indicator indicator-publish" title="Published">P</span>';
            }
            if (data.ignores && data.ignores.length > 0) {
                html += '<span class="indicator indicator-ignore" title="Has ' + data.ignores.length + ' ignored members">I</span>';
            }
            if (data.writable) {
                html += '<span class="indicator indicator-write" title="Writable">W</span>';
            }
            indicators.innerHTML = html;
        }

        // If this tag is currently selected, update the details panel
        if (typeof selectedInfo !== 'undefined' && selectedInfo &&
            selectedInfo.plc === data.plc && selectedInfo.tagName === data.tag && !selectedInfo.path) {
            selectedInfo.enabled = data.enabled;
            selectedInfo.writable = data.writable;
            selectedInfo.hasIgnores = data.ignores && data.ignores.length > 0;
            selectedInfo.ignoreCount = data.ignores ? data.ignores.length : 0;
            selectedInfo.ignoreList = data.ignores || [];
            if (typeof updateDetailsPanel === 'function') {
                updateDetailsPanel(selectedInfo);
            }
        }

        // Update ignored indicators in children if expanded
        updateChildIgnoreIndicators(tagItem, data.ignores || []);
    }

    function onStatusChange(data) {
        // Find the PLC group in the DOM
        var plcGroup = document.querySelector('.plc-group[data-plc="' + WarLink.escapeSelector(data.plc) + '"]');
        if (!plcGroup) return;

        // Update the status dot
        var statusDot = plcGroup.querySelector('.plc-header .status-dot');
        if (statusDot) {
            statusDot.className = 'status-dot status-' + data.status;
        }

        // Update tag count display
        var tagCountEl = plcGroup.querySelector('.plc-header .tag-count');
        if (tagCountEl && data.tagCount !== undefined) {
            tagCountEl.textContent = data.tagCount + ' tags';
        }

        // If tags were discovered (tagCount > 0) but tree has no tag items, refresh the tree
        if (data.tagCount > 0) {
            var hasTagItems = plcGroup.querySelector('.tag-item');
            if (!hasTagItems) {
                var tree = document.getElementById('republisher-tree');
                if (tree) {
                    fetch('/htmx/republisher').then(function(resp) {
                        if (!resp.ok) return;
                        return resp.text();
                    }).then(function(html) {
                        if (!html) return;
                        tree.innerHTML = html;
                        if (typeof restoreState === 'function') {
                            restoreState();
                        }
                    });
                }
            }
        }
    }

    function onEntityChange(data) {
        if (data.entityType !== 'plc') return;
        // When a PLC is added or removed, refresh the tree
        var tree = document.getElementById('republisher-tree');
        if (!tree) return;
        fetch('/htmx/republisher').then(function(resp) {
            if (!resp.ok) return;
            return resp.text();
        }).then(function(html) {
            if (!html) return;
            tree.innerHTML = html;
            // Restore collapsed/expanded state from localStorage
            if (typeof restoreState === 'function') {
                restoreState();
            }
        });
    }

    function updateExpandedChildren(tagItem, newValue) {
        if (typeof newValue !== 'object' || newValue === null) return;

        var ignoreList = [];
        try {
            ignoreList = JSON.parse(tagItem.dataset.ignoreList || '[]');
        } catch (e) {}

        // Find all expanded child rows and update their values
        tagItem.querySelectorAll('.tag-child-row').forEach(function(row) {
            var path = row.dataset.path;
            if (!path) return;

            // Navigate to this value in the new data
            var parts = path.split('.');
            var val = newValue;
            for (var i = 0; i < parts.length && val !== undefined; i++) {
                val = val[parts[i]];
            }

            if (val !== undefined) {
                row.dataset.json = JSON.stringify(val);

                // Update displayed value
                var valueSpan = row.querySelector('.child-value');
                if (valueSpan) {
                    if (typeof val === 'object' && val !== null) {
                        var isArray = Array.isArray(val);
                        var count = isArray ? val.length : Object.keys(val).length;
                        valueSpan.textContent = isArray ? '[' + count + ' items]' : '[' + count + ' fields]';
                    } else {
                        valueSpan.innerHTML = formatChildValueHtml(val);
                    }
                }
            }
        });
    }

    function updateChildIgnoreIndicators(tagItem, ignoreList) {
        tagItem.querySelectorAll('.tag-child-row').forEach(function(row) {
            var path = row.dataset.path;
            if (!path) return;

            var isIgnored = ignoreList.indexOf(path) >= 0;
            var indicator = row.querySelector('.indicator-ignore');

            if (isIgnored && !indicator) {
                // Add indicator
                var span = document.createElement('span');
                span.className = 'indicator indicator-ignore';
                span.title = 'Ignored';
                span.textContent = 'I';
                row.insertBefore(span, row.firstChild.nextSibling || row.firstChild);
                row.classList.add('ignored');
            } else if (!isIgnored && indicator) {
                // Remove indicator
                indicator.remove();
                row.classList.remove('ignored');
            }

            // Update data attribute for children
            row.dataset.ignoreList = JSON.stringify(ignoreList);
        });
    }

    function updateChildRowIndicators(row, published, writable, ignored) {
        // Handle publish indicator
        var publishIndicator = row.querySelector('.indicator-publish');
        if (published && !publishIndicator) {
            var span = document.createElement('span');
            span.className = 'indicator indicator-publish';
            span.title = 'Published';
            span.textContent = 'P';
            var expandBtn = row.querySelector('.child-expand');
            if (expandBtn) {
                expandBtn.after(span);
            } else {
                row.insertBefore(span, row.firstChild);
            }
        } else if (!published && publishIndicator) {
            publishIndicator.remove();
        }

        // Handle write indicator
        var writeIndicator = row.querySelector('.indicator-write');
        if (writable && !writeIndicator) {
            var span = document.createElement('span');
            span.className = 'indicator indicator-write';
            span.title = 'Writable';
            span.textContent = 'W';
            var after = row.querySelector('.indicator-publish') || row.querySelector('.child-expand');
            if (after) {
                after.after(span);
            } else {
                row.insertBefore(span, row.firstChild);
            }
        } else if (!writable && writeIndicator) {
            writeIndicator.remove();
        }
    }

    function formatValue(value) {
        if (value === null) return '-';
        if (typeof value === 'object') {
            if (Array.isArray(value)) {
                return '[' + value.length + ' items]';
            }
            return '{' + Object.keys(value).length + ' fields}';
        }
        if (typeof value === 'number') {
            if (Number.isInteger(value)) return String(value);
            return value.toFixed(4).replace(/\.?0+$/, '');
        }
        if (typeof value === 'string') {
            if (value.length > 30) return value.substring(0, 27) + '...';
            return value;
        }
        return String(value);
    }

    function formatChildValueHtml(value) {
        if (value === null) return '<span class="text-muted">null</span>';
        if (typeof value === 'boolean') {
            return value ? '<span class="text-success">true</span>' : '<span class="text-muted">false</span>';
        }
        if (typeof value === 'string') {
            var display = value.length > 20 ? value.substring(0, 20) + '...' : value;
            return '"' + WarLink.escapeHtml(display) + '"';
        }
        return String(value);
    }

    // Auto-connect when the script loads
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', connect);
    } else {
        connect();
    }

    // Public API
    return {
        connect: connect,
        isConnected: function() { return connected; }
    };
})();
