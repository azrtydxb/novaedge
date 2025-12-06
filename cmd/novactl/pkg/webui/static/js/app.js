// NovaEdge Dashboard Application
(function() {
    'use strict';

    // API base URL
    const API_BASE = '/api/v1';

    // State
    let currentPage = 'dashboard';
    let currentNamespace = 'all';
    let appMode = 'kubernetes';
    let isReadOnly = false;
    let selectedResources = new Set();
    let configHistory = [];
    let historyIndex = -1;
    const MAX_HISTORY = 50;

    // Expose currentNamespace for forms
    Object.defineProperty(window, 'currentNamespace', {
        get: () => currentNamespace
    });

    // Expose appMode for other modules
    Object.defineProperty(window, 'appMode', {
        get: () => appMode
    });

    // Resource type mapping
    const resourceTypes = {
        gateways: 'gateway',
        routes: 'route',
        backends: 'backend',
        vips: 'vip',
        policies: 'policy'
    };

    // Initialize the application
    function init() {
        setupNavigation();
        loadMode();
        loadNamespaces();
        showPage('dashboard');

        // Auto-refresh every 30 seconds
        setInterval(refreshCurrentPage, 30000);
    }

    // Load operating mode
    async function loadMode() {
        try {
            const response = await fetchAPI('/mode');
            appMode = response.mode || 'kubernetes';
            isReadOnly = response.readOnly || false;

            const indicator = document.getElementById('mode-indicator');
            if (indicator) {
                const modeLabel = appMode === 'kubernetes' ? 'Kubernetes' : 'Standalone';
                const readOnlyLabel = isReadOnly ? ' (Read-Only)' : '';
                indicator.innerHTML = `<span class="badge ${appMode === 'kubernetes' ? 'badge-info' : 'badge-warning'}">${modeLabel}${readOnlyLabel}</span>`;
            }

            // Hide write buttons if read-only
            updateWriteButtons();
        } catch (error) {
            console.error('Failed to load mode:', error);
        }
    }

    // Update write button visibility
    function updateWriteButtons() {
        const createBtn = document.getElementById('create-btn');
        if (createBtn) {
            createBtn.style.display = isReadOnly ? 'none' : 'inline-block';
        }
    }

    // Setup navigation
    function setupNavigation() {
        document.querySelectorAll('.nav-item').forEach(item => {
            item.addEventListener('click', () => {
                const page = item.dataset.page;
                showPage(page);
            });
        });

        document.getElementById('namespace').addEventListener('change', (e) => {
            currentNamespace = e.target.value;
            refreshCurrentPage();
        });
    }

    // Load namespaces
    async function loadNamespaces() {
        try {
            const namespaces = await fetchAPI('/namespaces');
            const select = document.getElementById('namespace');
            select.innerHTML = '<option value="all">All Namespaces</option>';
            namespaces.forEach(ns => {
                const option = document.createElement('option');
                option.value = ns;
                option.textContent = ns;
                select.appendChild(option);
            });
        } catch (error) {
            console.error('Failed to load namespaces:', error);
        }
    }

    // Show a page
    function showPage(page) {
        currentPage = page;

        // Update navigation
        document.querySelectorAll('.nav-item').forEach(item => {
            item.classList.toggle('active', item.dataset.page === page);
        });

        // Update page title
        const titles = {
            dashboard: 'Dashboard',
            gateways: 'Gateways',
            routes: 'Routes',
            backends: 'Backends',
            vips: 'VIPs',
            policies: 'Policies',
            agents: 'Agents'
        };
        document.getElementById('page-title').textContent = titles[page] || page;

        // Show/hide create and template buttons based on page
        const createBtn = document.getElementById('create-btn');
        const templateBtn = document.getElementById('template-btn');
        const canCreate = ['gateways', 'routes', 'backends', 'vips', 'policies'].includes(page);

        if (createBtn) {
            createBtn.style.display = (canCreate && !isReadOnly) ? 'inline-block' : 'none';
        }
        if (templateBtn) {
            templateBtn.style.display = (canCreate && !isReadOnly) ? 'inline-block' : 'none';
        }

        // Load page content
        loadPageContent(page);
    }
    window.showPage = showPage;

    // Handle create button click
    window.handleCreate = function() {
        const resourceType = resourceTypes[currentPage];
        if (resourceType && window.openCreateForm) {
            window.openCreateForm(resourceType);
        }
    };

    // Handle show templates click
    window.handleShowTemplates = function() {
        const resourceType = resourceTypes[currentPage];
        if (resourceType && window.showTemplates) {
            window.showTemplates(resourceType);
        }
    };

    // Refresh current page
    window.refreshCurrentPage = function() {
        loadPageContent(currentPage);
    };

    // Load page content
    async function loadPageContent(page) {
        const container = document.getElementById('page-content');
        container.innerHTML = '<div class="loading"><div class="spinner"></div></div>';

        try {
            switch (page) {
                case 'dashboard':
                    await loadDashboard(container);
                    break;
                case 'gateways':
                    await loadResources(container, 'gateways', 'Gateway');
                    break;
                case 'routes':
                    await loadResources(container, 'routes', 'Route');
                    break;
                case 'backends':
                    await loadResources(container, 'backends', 'Backend');
                    break;
                case 'vips':
                    await loadResources(container, 'vips', 'VIP');
                    break;
                case 'policies':
                    await loadResources(container, 'policies', 'Policy');
                    break;
                case 'agents':
                    await loadAgents(container);
                    break;
                default:
                    container.innerHTML = '<div class="empty-state"><h3>Page not found</h3></div>';
            }
        } catch (error) {
            container.innerHTML = `<div class="empty-state"><h3>Error loading data</h3><p>${error.message}</p></div>`;
        }
    }

    // Load dashboard
    async function loadDashboard(container) {
        let html = '<div class="metrics-grid">';

        // Try to load Prometheus metrics
        try {
            const metrics = await fetchAPI('/metrics/dashboard');

            html += createMetricCard('Request Rate',
                metrics.requestRate !== null ? `${metrics.requestRate.toFixed(2)} req/s` : 'N/A',
                '5-minute average');

            html += createMetricCard('Active Connections',
                metrics.activeConnections !== null ? Math.round(metrics.activeConnections).toString() : 'N/A',
                'Current connections');

            html += createMetricCard('Error Rate',
                metrics.errorRate !== null ? `${metrics.errorRate.toFixed(2)} req/s` : 'N/A',
                '5xx errors (5m avg)', metrics.errorRate > 0 ? 'error' : 'success');

            html += createMetricCard('Avg Latency',
                metrics.avgLatency !== null ? formatDuration(metrics.avgLatency) : 'N/A',
                'Request latency');

            html += createMetricCard('VIP Failovers',
                metrics.vipFailovers !== null ? Math.round(metrics.vipFailovers).toString() : 'N/A',
                'Last 24 hours', metrics.vipFailovers > 0 ? 'warning' : 'success');

            const agentHealth = metrics.healthyAgents !== null && metrics.totalAgents !== null
                ? `${Math.round(metrics.healthyAgents)} / ${Math.round(metrics.totalAgents)}`
                : 'N/A';
            html += createMetricCard('Agent Health',
                agentHealth,
                'Healthy / Total', metrics.healthyAgents === metrics.totalAgents ? 'success' : 'warning');
        } catch (error) {
            html += createMetricCard('Metrics', 'Unavailable', 'Prometheus not configured or unreachable');
        }

        html += '</div>';

        // Resource counts
        html += '<div class="metrics-grid">';

        try {
            const [gateways, routes, backends, vips] = await Promise.all([
                fetchAPI('/gateways?namespace=' + currentNamespace),
                fetchAPI('/routes?namespace=' + currentNamespace),
                fetchAPI('/backends?namespace=' + currentNamespace),
                fetchAPI('/vips?namespace=' + currentNamespace)
            ]);

            html += createMetricCard('Gateways', gateways.length.toString(), 'Total configured');
            html += createMetricCard('Routes', routes.length.toString(), 'Total configured');
            html += createMetricCard('Backends', backends.length.toString(), 'Total configured');
            html += createMetricCard('VIPs', vips.length.toString(), 'Total configured');
        } catch (error) {
            html += createMetricCard('Resources', 'Error', 'Failed to load resource counts');
        }

        html += '</div>';

        // Mode information
        html += `
            <div class="card">
                <div class="card-header">
                    <h3>Quick Overview</h3>
                </div>
                <div class="card-body">
                    <p>Select a resource type from the sidebar to view and manage NovaEdge configurations.</p>
                    <p style="margin-top: 8px;"><strong>Mode:</strong> <span class="badge ${appMode === 'kubernetes' ? 'badge-info' : 'badge-warning'}">${appMode === 'kubernetes' ? 'Kubernetes (CRD)' : 'Standalone (YAML)'}</span></p>
                    ${isReadOnly ? '<p style="margin-top: 8px;"><span class="badge badge-warning">Read-Only Mode</span> - Configuration changes are disabled.</p>' : ''}
                    <br>
                    <div class="detail-grid">
                        <div class="detail-item">
                            <div class="detail-label">Gateways</div>
                            <div class="detail-value">Define listeners, TLS configuration, and ingress points</div>
                        </div>
                        <div class="detail-item">
                            <div class="detail-label">Routes</div>
                            <div class="detail-value">Configure HTTP routing rules and traffic matching</div>
                        </div>
                        <div class="detail-item">
                            <div class="detail-label">Backends</div>
                            <div class="detail-value">Manage upstream services and load balancing</div>
                        </div>
                        <div class="detail-item">
                            <div class="detail-label">VIPs</div>
                            <div class="detail-value">Configure virtual IP addresses and failover</div>
                        </div>
                    </div>
                </div>
            </div>
        `;

        container.innerHTML = html;
    }

    // Create metric card HTML
    function createMetricCard(title, value, subtitle, valueClass = '') {
        return `
            <div class="metric-card">
                <h3>${escapeHtml(title)}</h3>
                <div class="metric-value ${valueClass}">${escapeHtml(value)}</div>
                <div class="metric-subtitle">${escapeHtml(subtitle)}</div>
            </div>
        `;
    }

    // Load resources (generic)
    async function loadResources(container, endpoint, resourceType) {
        const resources = await fetchAPI(`/${endpoint}?namespace=${currentNamespace}`);
        const singularType = resourceTypes[endpoint];
        selectedResources.clear();

        if (resources.length === 0) {
            container.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon">&#128196;</div>
                    <h3>No ${resourceType} resources found</h3>
                    <p>Create a ${resourceType} resource to get started.</p>
                    ${!isReadOnly ? `<button class="btn btn-primary" style="margin-top: 16px;" onclick="openCreateForm('${singularType}')">+ Create ${resourceType}</button>` : ''}
                </div>
            `;
            return;
        }

        let html = `
            <div class="card">
                <div class="card-header">
                    <h3>${resourceType} Resources (${resources.length})</h3>
                    ${!isReadOnly ? `
                    <div class="bulk-actions" id="bulk-actions" style="display: none;">
                        <span id="selected-count">0 selected</span>
                        <button class="btn btn-sm btn-danger" onclick="bulkDelete('${singularType}')">Delete Selected</button>
                        <button class="btn btn-sm btn-secondary" onclick="clearSelection()">Clear Selection</button>
                    </div>
                    ` : ''}
                </div>
                <div class="card-body">
                    <div class="table-container">
                        <table>
                            <thead>
                                <tr>
                                    ${!isReadOnly ? '<th><input type="checkbox" id="select-all" onchange="toggleSelectAll(this)"></th>' : ''}
                                    <th>Name</th>
                                    <th>Namespace</th>
                                    <th>Age</th>
                                    <th>Status</th>
                                    ${!isReadOnly ? '<th>Actions</th>' : ''}
                                </tr>
                            </thead>
                            <tbody>
        `;

        resources.forEach(resource => {
            // Handle both Kubernetes CRD format and unified model format
            const metadata = resource.metadata || {};
            const name = metadata.name || resource.name || 'N/A';
            const namespace = metadata.namespace || resource.namespace || 'N/A';
            const creationTimestamp = metadata.creationTimestamp;
            const status = resource.status || {};
            const conditions = status.conditions || [];
            const readyCondition = conditions.find(c => c.type === 'Ready' || c.type === 'Accepted');
            const isReady = readyCondition ? readyCondition.status === 'True' : (status.ready !== false);
            const resourceKey = `${namespace}/${name}`;

            html += `
                <tr data-resource="${escapeHtml(resourceKey)}">
                    ${!isReadOnly ? `<td><input type="checkbox" class="resource-checkbox" value="${escapeHtml(resourceKey)}" onchange="toggleResourceSelection(this)"></td>` : ''}
                    <td class="clickable" onclick="showResourceDetail('${endpoint}', '${namespace}', '${name}')">
                        <strong>${escapeHtml(name)}</strong>
                    </td>
                    <td>${escapeHtml(namespace)}</td>
                    <td>${formatAge(creationTimestamp)}</td>
                    <td><span class="badge ${isReady ? 'badge-success' : 'badge-warning'}">${isReady ? 'Ready' : 'Pending'}</span></td>
                    ${!isReadOnly ? `
                    <td class="action-buttons">
                        <button class="btn btn-sm btn-secondary" onclick="editResource('${singularType}', '${endpoint}', '${namespace}', '${name}')">Edit</button>
                        <button class="btn btn-sm btn-secondary" onclick="openYamlEditor('${singularType}', '${endpoint}', '${namespace}', '${name}')" title="Edit YAML">YAML</button>
                        <button class="btn btn-sm btn-danger" onclick="deleteResource('${singularType}', '${namespace}', '${name}')">Delete</button>
                    </td>
                    ` : ''}
                </tr>
            `;
        });

        html += `
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        `;

        container.innerHTML = html;
    }

    // Toggle select all resources
    window.toggleSelectAll = function(checkbox) {
        const checkboxes = document.querySelectorAll('.resource-checkbox');
        checkboxes.forEach(cb => {
            cb.checked = checkbox.checked;
            const key = cb.value;
            if (checkbox.checked) {
                selectedResources.add(key);
            } else {
                selectedResources.delete(key);
            }
        });
        updateBulkActionsUI();
    };

    // Toggle single resource selection
    window.toggleResourceSelection = function(checkbox) {
        const key = checkbox.value;
        if (checkbox.checked) {
            selectedResources.add(key);
        } else {
            selectedResources.delete(key);
        }
        updateBulkActionsUI();
    };

    // Clear selection
    window.clearSelection = function() {
        selectedResources.clear();
        document.querySelectorAll('.resource-checkbox').forEach(cb => cb.checked = false);
        document.getElementById('select-all').checked = false;
        updateBulkActionsUI();
    };

    // Update bulk actions UI
    function updateBulkActionsUI() {
        const bulkActions = document.getElementById('bulk-actions');
        const selectedCount = document.getElementById('selected-count');
        if (bulkActions && selectedCount) {
            if (selectedResources.size > 0) {
                bulkActions.style.display = 'flex';
                selectedCount.textContent = `${selectedResources.size} selected`;
            } else {
                bulkActions.style.display = 'none';
            }
        }
    }

    // Bulk delete
    window.bulkDelete = async function(resourceType) {
        if (selectedResources.size === 0) return;

        const count = selectedResources.size;
        if (!confirm(`Are you sure you want to delete ${count} ${resourceType}(s)?`)) {
            return;
        }

        const endpoint = resourceType + 's';
        const errors = [];
        let deleted = 0;

        for (const key of selectedResources) {
            const [namespace, name] = key.split('/');
            try {
                const response = await fetch(`/api/v1/${endpoint}/${namespace}/${name}`, {
                    method: 'DELETE'
                });
                if (!response.ok) {
                    const error = await response.json();
                    errors.push(`${name}: ${error.error || 'Delete failed'}`);
                } else {
                    deleted++;
                }
            } catch (error) {
                errors.push(`${name}: ${error.message}`);
            }
        }

        selectedResources.clear();

        if (errors.length > 0) {
            showToast('warning', `Deleted ${deleted}/${count}. Errors: ${errors.slice(0, 3).join(', ')}${errors.length > 3 ? '...' : ''}`);
        } else {
            showToast('success', `Successfully deleted ${deleted} ${resourceType}(s)`);
        }

        window.refreshCurrentPage();
    };

    // Edit resource
    window.editResource = async function(resourceType, endpoint, namespace, name) {
        try {
            const resource = await fetchAPI(`/${endpoint}/${namespace}/${name}`);
            // Convert to unified format if needed
            const data = convertToUnifiedFormat(resource, resourceType);
            if (window.openEditForm) {
                window.openEditForm(resourceType, data);
            }
        } catch (error) {
            if (window.showToast) {
                window.showToast('error', 'Failed to load resource: ' + error.message);
            }
        }
    };

    // Convert CRD format to unified format
    function convertToUnifiedFormat(resource, resourceType) {
        if (resource.metadata) {
            // Kubernetes CRD format
            return {
                name: resource.metadata.name,
                namespace: resource.metadata.namespace,
                resourceVersion: resource.metadata.resourceVersion,
                ...resource.spec
            };
        }
        // Already in unified format
        return resource;
    }

    // Show resource detail
    window.showResourceDetail = async function(endpoint, namespace, name) {
        const container = document.getElementById('page-content');
        container.innerHTML = '<div class="loading"><div class="spinner"></div></div>';

        try {
            const resource = await fetchAPI(`/${endpoint}/${namespace}/${name}`);
            const singularType = resourceTypes[endpoint];

            let html = `
                <div class="card">
                    <div class="card-header">
                        <h3>${escapeHtml(name)}</h3>
                        <div class="header-actions">
                            ${!isReadOnly ? `
                            <button class="btn btn-secondary" onclick="editResource('${singularType}', '${endpoint}', '${namespace}', '${name}')">Edit</button>
                            <button class="btn btn-danger" onclick="deleteResource('${singularType}', '${namespace}', '${name}'); showPage('${endpoint}');">Delete</button>
                            ` : ''}
                            <button class="refresh-btn" onclick="showPage('${endpoint}')">Back to List</button>
                        </div>
                    </div>
                    <div class="card-body">
                        <div class="tabs">
                            <div class="tab active" onclick="showTab(this, 'overview')">Overview</div>
                            <div class="tab" onclick="showTab(this, 'spec')">Spec</div>
                            <div class="tab" onclick="showTab(this, 'status')">Status</div>
                            <div class="tab" onclick="showTab(this, 'yaml')">YAML</div>
                        </div>

                        <div id="tab-overview" class="tab-content">
                            ${renderResourceOverview(resource)}
                        </div>

                        <div id="tab-spec" class="tab-content" style="display: none;">
                            <div class="code-block">${escapeHtml(JSON.stringify(resource.spec || resource, null, 2))}</div>
                        </div>

                        <div id="tab-status" class="tab-content" style="display: none;">
                            <div class="code-block">${escapeHtml(JSON.stringify(resource.status || {}, null, 2))}</div>
                        </div>

                        <div id="tab-yaml" class="tab-content" style="display: none;">
                            <div class="code-block">${escapeHtml(jsonToYaml(resource))}</div>
                        </div>
                    </div>
                </div>
            `;

            container.innerHTML = html;
        } catch (error) {
            container.innerHTML = `<div class="empty-state"><h3>Error loading resource</h3><p>${error.message}</p></div>`;
        }
    };

    // Show tab
    window.showTab = function(tabElement, tabId) {
        // Update tab buttons
        tabElement.parentElement.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
        tabElement.classList.add('active');

        // Update tab content
        tabElement.closest('.card-body').querySelectorAll('.tab-content').forEach(c => c.style.display = 'none');
        document.getElementById('tab-' + tabId).style.display = 'block';
    };

    // Render resource overview
    function renderResourceOverview(resource) {
        const metadata = resource.metadata || {};
        const spec = resource.spec || resource;
        const status = resource.status || {};

        let html = '<div class="detail-grid">';

        // Basic metadata
        html += `
            <div class="detail-item">
                <div class="detail-label">Name</div>
                <div class="detail-value">${escapeHtml(metadata.name || resource.name || 'N/A')}</div>
            </div>
            <div class="detail-item">
                <div class="detail-label">Namespace</div>
                <div class="detail-value">${escapeHtml(metadata.namespace || resource.namespace || 'N/A')}</div>
            </div>
        `;

        if (metadata.uid) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">UID</div>
                    <div class="detail-value">${escapeHtml(metadata.uid)}</div>
                </div>
            `;
        }

        if (metadata.creationTimestamp) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Created</div>
                    <div class="detail-value">${escapeHtml(metadata.creationTimestamp)}</div>
                </div>
            `;
        }

        // Spec-specific fields
        if (spec.listeners) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Listeners</div>
                    <div class="detail-value">${spec.listeners.length} configured</div>
                </div>
            `;
        }

        if (spec.hostnames) {
            const hostnames = Array.isArray(spec.hostnames) ? spec.hostnames.join(', ') : spec.hostnames;
            html += `
                <div class="detail-item">
                    <div class="detail-label">Hostnames</div>
                    <div class="detail-value">${escapeHtml(hostnames || 'N/A')}</div>
                </div>
            `;
        }

        if (spec.backendRefs) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Backends</div>
                    <div class="detail-value">${spec.backendRefs.length} configured</div>
                </div>
            `;
        }

        if (spec.endpoints) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Endpoints</div>
                    <div class="detail-value">${spec.endpoints.length} configured</div>
                </div>
            `;
        }

        if (spec.lbPolicy) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">LB Policy</div>
                    <div class="detail-value">${escapeHtml(spec.lbPolicy)}</div>
                </div>
            `;
        }

        if (spec.address) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Address</div>
                    <div class="detail-value">${escapeHtml(spec.address)}</div>
                </div>
            `;
        }

        if (spec.mode) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Mode</div>
                    <div class="detail-value">${escapeHtml(spec.mode)}</div>
                </div>
            `;
        }

        if (spec.type) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Policy Type</div>
                    <div class="detail-value">${escapeHtml(spec.type)}</div>
                </div>
            `;
        }

        html += '</div>';

        // Show conditions
        if (status.conditions && status.conditions.length > 0) {
            html += `
                <h4 style="margin-top: 20px; margin-bottom: 12px;">Conditions</h4>
                <div class="table-container">
                    <table>
                        <thead>
                            <tr>
                                <th>Type</th>
                                <th>Status</th>
                                <th>Reason</th>
                                <th>Message</th>
                            </tr>
                        </thead>
                        <tbody>
            `;

            status.conditions.forEach(condition => {
                const badgeClass = condition.status === 'True' ? 'badge-success' : 'badge-warning';
                html += `
                    <tr>
                        <td>${escapeHtml(condition.type)}</td>
                        <td><span class="badge ${badgeClass}">${escapeHtml(condition.status)}</span></td>
                        <td>${escapeHtml(condition.reason || '-')}</td>
                        <td>${escapeHtml(condition.message || '-')}</td>
                    </tr>
                `;
            });

            html += '</tbody></table></div>';
        }

        // Show labels
        if (metadata.labels && Object.keys(metadata.labels).length > 0) {
            html += `<h4 style="margin-top: 20px; margin-bottom: 12px;">Labels</h4><div class="tags">`;
            Object.entries(metadata.labels).forEach(([key, value]) => {
                html += `<span class="tag">${escapeHtml(key)}: ${escapeHtml(value)}</span>`;
            });
            html += '</div>';
        }

        return html;
    }

    // Load agents
    async function loadAgents(container) {
        try {
            const agents = await fetchAPI('/agents');

            if (!agents || agents.length === 0) {
                container.innerHTML = `
                    <div class="empty-state">
                        <div class="empty-state-icon">&#9679;</div>
                        <h3>No agents found</h3>
                        <p>${appMode === 'kubernetes' ? 'NovaEdge agents run as a DaemonSet on cluster nodes.' : 'Agent information is not available in standalone mode.'}</p>
                    </div>
                `;
                return;
            }

            let html = `
                <div class="card">
                    <div class="card-header">
                        <h3>NovaEdge Agents (${agents.length})</h3>
                    </div>
                    <div class="card-body">
                        <div class="table-container">
                            <table>
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Node</th>
                                        <th>Pod IP</th>
                                        <th>Phase</th>
                                        <th>Ready</th>
                                        <th>Age</th>
                                    </tr>
                                </thead>
                                <tbody>
            `;

            agents.forEach(agent => {
                const readyClass = agent.ready ? 'badge-success' : 'badge-error';
                const phaseClass = agent.phase === 'Running' ? 'badge-success' :
                                  agent.phase === 'Pending' ? 'badge-warning' : 'badge-error';

                html += `
                    <tr>
                        <td><strong>${escapeHtml(agent.name)}</strong></td>
                        <td>${escapeHtml(agent.nodeName || 'N/A')}</td>
                        <td><code>${escapeHtml(agent.podIP || 'N/A')}</code></td>
                        <td><span class="badge ${phaseClass}">${escapeHtml(agent.phase)}</span></td>
                        <td><span class="badge ${readyClass}">${agent.ready ? 'Ready' : 'Not Ready'}</span></td>
                        <td>${formatAge(agent.startTime)}</td>
                    </tr>
                `;
            });

            html += `
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
            `;

            container.innerHTML = html;
        } catch (error) {
            container.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon">&#9679;</div>
                    <h3>Unable to load agents</h3>
                    <p>${error.message}</p>
                </div>
            `;
        }
    }

    // Export config
    window.exportConfig = async function() {
        try {
            const response = await fetch(`${API_BASE}/config/export?namespace=${currentNamespace}`, {
                method: 'POST'
            });

            if (!response.ok) {
                throw new Error('Export failed');
            }

            const blob = await response.blob();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'novaedge-config.yaml';
            document.body.appendChild(a);
            a.click();
            window.URL.revokeObjectURL(url);
            a.remove();

            if (window.showToast) {
                window.showToast('success', 'Configuration exported successfully');
            }
        } catch (error) {
            if (window.showToast) {
                window.showToast('error', 'Export failed: ' + error.message);
            }
        }
    };

    // Show import dialog
    window.showImportDialog = function() {
        if (isReadOnly) {
            if (window.showToast) {
                window.showToast('error', 'Import is disabled in read-only mode');
            }
            return;
        }

        const html = `
            <div class="modal-overlay" onclick="closeModal()">
                <div class="modal" onclick="event.stopPropagation()">
                    <div class="modal-header">
                        <h3>Import Configuration</h3>
                        <button class="modal-close" onclick="closeModal()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <p>Upload a YAML configuration file to import.</p>
                        <div class="form-group">
                            <input type="file" id="import-file" accept=".yaml,.yml" class="form-input">
                        </div>
                        <div class="form-group">
                            <label class="form-label">
                                <input type="checkbox" id="dry-run-checkbox"> Dry Run (preview changes without applying)
                            </label>
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="closeModal()">Cancel</button>
                        <button class="btn btn-primary" onclick="importConfig()">Import</button>
                    </div>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', html);
    };

    // Import config
    window.importConfig = async function() {
        const fileInput = document.getElementById('import-file');
        const dryRunCheckbox = document.getElementById('dry-run-checkbox');

        if (!fileInput.files || fileInput.files.length === 0) {
            if (window.showToast) {
                window.showToast('error', 'Please select a file');
            }
            return;
        }

        const file = fileInput.files[0];
        const dryRun = dryRunCheckbox.checked;

        try {
            const content = await file.text();
            const response = await fetch(`${API_BASE}/config/import?dryRun=${dryRun}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-yaml' },
                body: content
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Import failed');
            }

            const result = await response.json();

            if (dryRun) {
                let message = `Dry run results:\n`;
                message += `- ${result.created?.length || 0} resources would be created\n`;
                message += `- ${result.updated?.length || 0} resources would be updated`;
                alert(message);
            } else {
                if (window.showToast) {
                    window.showToast('success', `Import successful: ${result.created?.length || 0} created, ${result.updated?.length || 0} updated`);
                }
                window.closeModal();
                window.refreshCurrentPage();
            }
        } catch (error) {
            if (window.showToast) {
                window.showToast('error', 'Import failed: ' + error.message);
            }
        }
    };

    // YAML Editor Mode
    window.openYamlEditor = async function(resourceType, endpoint, namespace, name) {
        try {
            const resource = await fetchAPI(`/${endpoint}/${namespace}/${name}`);
            const yamlContent = jsonToYaml(resource);
            const originalYaml = yamlContent;

            const html = `
                <div class="modal-overlay yaml-editor-modal" onclick="closeModal()">
                    <div class="modal modal-large" onclick="event.stopPropagation()">
                        <div class="modal-header">
                            <h3>Edit YAML - ${escapeHtml(name)}</h3>
                            <button class="modal-close" onclick="closeModal()">&times;</button>
                        </div>
                        <div class="modal-body">
                            <div class="yaml-editor-container">
                                <textarea id="yaml-editor" class="yaml-editor" spellcheck="false">${escapeHtml(yamlContent)}</textarea>
                            </div>
                            <div class="yaml-editor-actions">
                                <button class="btn btn-sm btn-secondary" onclick="formatYaml()">Format</button>
                                <button class="btn btn-sm btn-secondary" onclick="validateYamlSyntax()">Validate</button>
                                <button class="btn btn-sm btn-secondary" onclick="showYamlDiff('${escapeHtml(originalYaml.replace(/'/g, "\\'"))}')">Show Diff</button>
                            </div>
                            <div id="yaml-validation-result" class="yaml-validation-result"></div>
                        </div>
                        <div class="modal-footer">
                            <button class="btn btn-secondary" onclick="closeModal()">Cancel</button>
                            <button class="btn btn-primary" onclick="saveYamlChanges('${resourceType}', '${endpoint}', '${namespace}', '${name}')">Save Changes</button>
                        </div>
                    </div>
                </div>
            `;

            document.body.insertAdjacentHTML('beforeend', html);

            // Focus editor
            const editor = document.getElementById('yaml-editor');
            if (editor) {
                editor.focus();
                // Handle tab key for indentation
                editor.addEventListener('keydown', handleYamlEditorKeydown);
            }
        } catch (error) {
            showToast('error', 'Failed to load resource: ' + error.message);
        }
    };

    // Handle keydown in YAML editor
    function handleYamlEditorKeydown(e) {
        if (e.key === 'Tab') {
            e.preventDefault();
            const start = this.selectionStart;
            const end = this.selectionEnd;
            const value = this.value;
            this.value = value.substring(0, start) + '  ' + value.substring(end);
            this.selectionStart = this.selectionEnd = start + 2;
        }
    }

    // Format YAML
    window.formatYaml = function() {
        const editor = document.getElementById('yaml-editor');
        if (!editor) return;

        try {
            const obj = yamlToJson(editor.value);
            editor.value = jsonToYaml(obj);
            showToast('success', 'YAML formatted');
        } catch (error) {
            showToast('error', 'Invalid YAML: ' + error.message);
        }
    };

    // Validate YAML syntax
    window.validateYamlSyntax = function() {
        const editor = document.getElementById('yaml-editor');
        const resultDiv = document.getElementById('yaml-validation-result');
        if (!editor || !resultDiv) return;

        try {
            yamlToJson(editor.value);
            resultDiv.innerHTML = '<span class="validation-success">✓ Valid YAML syntax</span>';
            resultDiv.className = 'yaml-validation-result success';
        } catch (error) {
            resultDiv.innerHTML = '<span class="validation-error">✗ ' + escapeHtml(error.message) + '</span>';
            resultDiv.className = 'yaml-validation-result error';
        }
    };

    // Show YAML diff
    window.showYamlDiff = function(originalYaml) {
        const editor = document.getElementById('yaml-editor');
        if (!editor) return;

        const currentYaml = editor.value;
        const diff = computeSimpleDiff(originalYaml, currentYaml);

        const html = `
            <div class="modal-overlay diff-modal" onclick="closeDiffModal()">
                <div class="modal modal-large" onclick="event.stopPropagation()">
                    <div class="modal-header">
                        <h3>Configuration Diff</h3>
                        <button class="modal-close" onclick="closeDiffModal()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="diff-container">
                            <pre class="diff-content">${diff}</pre>
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="closeDiffModal()">Close</button>
                    </div>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', html);
    };

    // Close diff modal
    window.closeDiffModal = function() {
        const modal = document.querySelector('.diff-modal');
        if (modal) modal.remove();
    };

    // Compute simple diff
    function computeSimpleDiff(original, current) {
        const origLines = original.split('\n');
        const currLines = current.split('\n');
        let html = '';

        const maxLines = Math.max(origLines.length, currLines.length);
        for (let i = 0; i < maxLines; i++) {
            const origLine = origLines[i] || '';
            const currLine = currLines[i] || '';

            if (origLine === currLine) {
                html += `<div class="diff-line diff-unchanged">  ${escapeHtml(currLine)}</div>`;
            } else if (!origLines[i]) {
                html += `<div class="diff-line diff-added">+ ${escapeHtml(currLine)}</div>`;
            } else if (!currLines[i]) {
                html += `<div class="diff-line diff-removed">- ${escapeHtml(origLine)}</div>`;
            } else {
                html += `<div class="diff-line diff-removed">- ${escapeHtml(origLine)}</div>`;
                html += `<div class="diff-line diff-added">+ ${escapeHtml(currLine)}</div>`;
            }
        }

        return html;
    }

    // Save YAML changes
    window.saveYamlChanges = async function(resourceType, endpoint, namespace, name) {
        const editor = document.getElementById('yaml-editor');
        if (!editor) return;

        try {
            const obj = yamlToJson(editor.value);

            // Save to history before making changes
            saveToHistory(resourceType, endpoint, namespace, name, 'update');

            const response = await fetch(`/api/v1/${endpoint}/${namespace}/${name}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(obj)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Save failed');
            }

            showToast('success', 'Changes saved successfully');
            closeModal();
            window.refreshCurrentPage();
        } catch (error) {
            showToast('error', 'Save failed: ' + error.message);
        }
    };

    // Simple YAML parser (basic implementation)
    function yamlToJson(yaml) {
        // This is a simplified YAML parser for basic structures
        // For production, you'd want to use a proper library like js-yaml
        const lines = yaml.split('\n');
        const result = {};
        const stack = [{ obj: result, indent: -1 }];
        let currentArray = null;
        let currentArrayIndent = -1;

        for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            if (line.trim() === '' || line.trim().startsWith('#')) continue;

            const indent = line.search(/\S/);
            const content = line.trim();

            // Handle array items
            if (content.startsWith('- ')) {
                const value = content.substring(2).trim();
                if (currentArray && indent >= currentArrayIndent) {
                    if (value.includes(':')) {
                        // Object in array
                        const obj = {};
                        const [key, val] = value.split(':').map(s => s.trim());
                        obj[key] = parseYamlValue(val);
                        currentArray.push(obj);
                    } else {
                        currentArray.push(parseYamlValue(value));
                    }
                }
                continue;
            }

            // Handle key: value
            const colonIndex = content.indexOf(':');
            if (colonIndex > 0) {
                const key = content.substring(0, colonIndex).trim();
                const value = content.substring(colonIndex + 1).trim();

                // Pop stack until we find the right indent level
                while (stack.length > 1 && stack[stack.length - 1].indent >= indent) {
                    stack.pop();
                }

                const parent = stack[stack.length - 1].obj;

                if (value === '' || value === '|' || value === '>') {
                    // Nested object or multiline string
                    parent[key] = {};
                    stack.push({ obj: parent[key], indent: indent });
                } else if (value === '[]') {
                    parent[key] = [];
                    currentArray = parent[key];
                    currentArrayIndent = indent;
                } else {
                    parent[key] = parseYamlValue(value);
                    currentArray = null;
                }

                // Check if next lines are array items
                if (i + 1 < lines.length) {
                    const nextLine = lines[i + 1];
                    const nextIndent = nextLine.search(/\S/);
                    if (nextLine.trim().startsWith('- ') && nextIndent > indent) {
                        parent[key] = [];
                        currentArray = parent[key];
                        currentArrayIndent = nextIndent;
                    }
                }
            }
        }

        return result;
    }

    // Parse YAML value
    function parseYamlValue(value) {
        if (value === 'true') return true;
        if (value === 'false') return false;
        if (value === 'null' || value === '~') return null;
        if (/^-?\d+$/.test(value)) return parseInt(value, 10);
        if (/^-?\d*\.\d+$/.test(value)) return parseFloat(value);
        // Remove quotes
        if ((value.startsWith('"') && value.endsWith('"')) ||
            (value.startsWith("'") && value.endsWith("'"))) {
            return value.slice(1, -1);
        }
        return value;
    }

    // Config Templates
    const ConfigTemplates = {
        gateway: {
            'Basic HTTP Gateway': {
                name: 'http-gateway',
                namespace: 'default',
                listeners: [
                    { name: 'http', port: 80, protocol: 'HTTP' }
                ]
            },
            'HTTPS Gateway with TLS': {
                name: 'https-gateway',
                namespace: 'default',
                listeners: [
                    { name: 'https', port: 443, protocol: 'HTTPS', tls: { mode: 'Terminate', certificateRef: { name: 'tls-cert' } } }
                ]
            },
            'Gateway with Tracing': {
                name: 'traced-gateway',
                namespace: 'default',
                listeners: [
                    { name: 'http', port: 80, protocol: 'HTTP' }
                ],
                tracing: { enabled: true, samplingRate: 10 }
            }
        },
        route: {
            'Simple Path Route': {
                name: 'api-route',
                namespace: 'default',
                hostnames: ['api.example.com'],
                matches: [{ path: { type: 'PathPrefix', value: '/api' } }],
                backendRefs: [{ name: 'api-backend', weight: 100 }]
            },
            'Canary Deployment Route': {
                name: 'canary-route',
                namespace: 'default',
                hostnames: ['app.example.com'],
                matches: [{ path: { type: 'PathPrefix', value: '/' } }],
                backendRefs: [
                    { name: 'stable-backend', weight: 90 },
                    { name: 'canary-backend', weight: 10 }
                ]
            },
            'Method-based Route': {
                name: 'method-route',
                namespace: 'default',
                matches: [
                    { path: { type: 'PathPrefix', value: '/api' }, method: 'GET' },
                    { path: { type: 'PathPrefix', value: '/api' }, method: 'POST' }
                ],
                backendRefs: [{ name: 'api-backend' }]
            }
        },
        backend: {
            'Round Robin Backend': {
                name: 'rr-backend',
                namespace: 'default',
                lbPolicy: 'RoundRobin',
                endpoints: [
                    { address: 'service-1:8080', weight: 1 },
                    { address: 'service-2:8080', weight: 1 }
                ]
            },
            'Backend with Health Check': {
                name: 'healthy-backend',
                namespace: 'default',
                lbPolicy: 'P2C',
                endpoints: [
                    { address: 'service:8080' }
                ],
                healthCheck: {
                    enabled: true,
                    protocol: 'HTTP',
                    path: '/health',
                    interval: '10s',
                    timeout: '5s'
                }
            },
            'Weighted Backend': {
                name: 'weighted-backend',
                namespace: 'default',
                lbPolicy: 'RoundRobin',
                endpoints: [
                    { address: 'primary:8080', weight: 80 },
                    { address: 'secondary:8080', weight: 20 }
                ]
            }
        },
        vip: {
            'L2 VIP': {
                name: 'l2-vip',
                namespace: 'default',
                address: '192.168.1.100/32',
                mode: 'L2',
                interface: 'eth0'
            },
            'BGP VIP': {
                name: 'bgp-vip',
                namespace: 'default',
                address: '10.0.0.100/32',
                mode: 'BGP',
                bgp: { localAS: 65001, peerAS: 65000, peerIP: '10.0.0.1' }
            }
        },
        policy: {
            'Rate Limit Policy': {
                name: 'rate-limit',
                namespace: 'default',
                type: 'RateLimit',
                rateLimit: { requestsPerSecond: 100, burstSize: 200, key: 'client_ip' }
            },
            'CORS Policy': {
                name: 'cors-policy',
                namespace: 'default',
                type: 'CORS',
                cors: {
                    allowOrigins: ['https://example.com'],
                    allowMethods: ['GET', 'POST', 'PUT', 'DELETE'],
                    allowHeaders: ['Content-Type', 'Authorization'],
                    allowCredentials: true
                }
            },
            'IP Whitelist Policy': {
                name: 'ip-whitelist',
                namespace: 'default',
                type: 'IPFilter',
                ipFilter: { allowList: ['10.0.0.0/8', '192.168.0.0/16'] }
            }
        }
    };

    // Show template selector
    window.showTemplates = function(resourceType) {
        const templates = ConfigTemplates[resourceType];
        if (!templates) {
            showToast('error', 'No templates available for this resource type');
            return;
        }

        let html = `
            <div class="modal-overlay template-modal" onclick="closeModal()">
                <div class="modal" onclick="event.stopPropagation()">
                    <div class="modal-header">
                        <h3>Select Template</h3>
                        <button class="modal-close" onclick="closeModal()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="template-list">
        `;

        Object.entries(templates).forEach(([name, config]) => {
            html += `
                <div class="template-item" onclick="applyTemplate('${resourceType}', '${escapeHtml(name)}')">
                    <div class="template-name">${escapeHtml(name)}</div>
                    <div class="template-preview">${escapeHtml(JSON.stringify(config, null, 2).substring(0, 100))}...</div>
                </div>
            `;
        });

        html += `
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="closeModal()">Cancel</button>
                    </div>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', html);
    };

    // Apply template
    window.applyTemplate = function(resourceType, templateName) {
        const templates = ConfigTemplates[resourceType];
        const template = templates[templateName];
        if (!template) return;

        closeModal();

        // Open edit form with template data
        if (window.openEditForm) {
            // Clone template and modify for create
            const data = JSON.parse(JSON.stringify(template));
            window.openCreateFormWithData(resourceType, data);
        }
    };

    // Open create form with pre-filled data
    window.openCreateFormWithData = function(resourceType, data) {
        if (window.openEditForm) {
            // Use edit form in "create" mode with pre-filled data
            window.openEditForm(resourceType, data);
            // Update title to show it's a create operation
            const header = document.querySelector('.modal-header h3');
            if (header) {
                header.textContent = 'Create from Template';
            }
        }
    };

    // Config History Management
    function saveToHistory(resourceType, endpoint, namespace, name, action) {
        if (appMode !== 'standalone') return; // Only for standalone mode

        const entry = {
            timestamp: new Date().toISOString(),
            resourceType,
            endpoint,
            namespace,
            name,
            action
        };

        // Get current state before change
        fetchAPI(`/${endpoint}/${namespace}/${name}`).then(resource => {
            entry.previousState = resource;
            configHistory.push(entry);

            // Trim history if too long
            if (configHistory.length > MAX_HISTORY) {
                configHistory.shift();
            }

            historyIndex = configHistory.length - 1;
        }).catch(() => {
            // Resource might be new, no previous state
            configHistory.push(entry);
            historyIndex = configHistory.length - 1;
        });
    }

    // Show config history
    window.showConfigHistory = function() {
        if (configHistory.length === 0) {
            showToast('info', 'No configuration history available');
            return;
        }

        let html = `
            <div class="modal-overlay history-modal" onclick="closeModal()">
                <div class="modal modal-large" onclick="event.stopPropagation()">
                    <div class="modal-header">
                        <h3>Configuration History</h3>
                        <button class="modal-close" onclick="closeModal()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="history-list">
        `;

        configHistory.slice().reverse().forEach((entry, index) => {
            const realIndex = configHistory.length - 1 - index;
            const date = new Date(entry.timestamp);
            const timeStr = date.toLocaleString();

            html += `
                <div class="history-item ${realIndex === historyIndex ? 'current' : ''}">
                    <div class="history-time">${escapeHtml(timeStr)}</div>
                    <div class="history-action">
                        <span class="badge badge-${entry.action === 'delete' ? 'error' : 'info'}">${escapeHtml(entry.action)}</span>
                        ${escapeHtml(entry.resourceType)} - ${escapeHtml(entry.namespace)}/${escapeHtml(entry.name)}
                    </div>
                    ${entry.previousState ? `
                    <div class="history-actions">
                        <button class="btn btn-sm btn-secondary" onclick="previewHistoryState(${realIndex})">Preview</button>
                        <button class="btn btn-sm btn-primary" onclick="restoreFromHistory(${realIndex})">Restore</button>
                    </div>
                    ` : ''}
                </div>
            `;
        });

        html += `
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="closeModal()">Close</button>
                        <button class="btn btn-danger" onclick="clearHistory()">Clear History</button>
                    </div>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', html);
    };

    // Preview history state
    window.previewHistoryState = function(index) {
        const entry = configHistory[index];
        if (!entry || !entry.previousState) return;

        const yaml = jsonToYaml(entry.previousState);

        const html = `
            <div class="modal-overlay preview-modal" onclick="closePreviewModal()">
                <div class="modal modal-large" onclick="event.stopPropagation()">
                    <div class="modal-header">
                        <h3>Previous State Preview</h3>
                        <button class="modal-close" onclick="closePreviewModal()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="code-block">${escapeHtml(yaml)}</div>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="closePreviewModal()">Close</button>
                    </div>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', html);
    };

    // Close preview modal
    window.closePreviewModal = function() {
        const modal = document.querySelector('.preview-modal');
        if (modal) modal.remove();
    };

    // Restore from history
    window.restoreFromHistory = async function(index) {
        const entry = configHistory[index];
        if (!entry || !entry.previousState) {
            showToast('error', 'Cannot restore: no previous state available');
            return;
        }

        if (!confirm(`Restore ${entry.resourceType} "${entry.name}" to previous state?`)) {
            return;
        }

        try {
            const response = await fetch(`/api/v1/${entry.endpoint}/${entry.namespace}/${entry.name}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(entry.previousState)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Restore failed');
            }

            showToast('success', 'Configuration restored successfully');
            closeModal();
            window.refreshCurrentPage();
        } catch (error) {
            showToast('error', 'Restore failed: ' + error.message);
        }
    };

    // Clear history
    window.clearHistory = function() {
        if (!confirm('Clear all configuration history?')) return;
        configHistory = [];
        historyIndex = -1;
        closeModal();
        showToast('success', 'History cleared');
    };

    // Undo last change
    window.undoLastChange = async function() {
        if (historyIndex < 0 || configHistory.length === 0) {
            showToast('info', 'Nothing to undo');
            return;
        }

        const entry = configHistory[historyIndex];
        if (!entry.previousState) {
            showToast('error', 'Cannot undo: no previous state');
            return;
        }

        await restoreFromHistory(historyIndex);
        historyIndex--;
    };

    // Fetch from API
    async function fetchAPI(path) {
        const response = await fetch(API_BASE + path);
        if (!response.ok) {
            const error = await response.json().catch(() => ({ error: response.statusText }));
            throw new Error(error.error || 'API request failed');
        }
        return response.json();
    }

    // Utility functions
    function escapeHtml(str) {
        if (str === null || str === undefined) return '';
        const div = document.createElement('div');
        div.textContent = String(str);
        return div.innerHTML;
    }

    function formatAge(timestamp) {
        if (!timestamp) return 'N/A';
        const date = new Date(timestamp);
        const now = new Date();
        const diffMs = now - date;
        const diffMins = Math.floor(diffMs / 60000);
        const diffHours = Math.floor(diffMins / 60);
        const diffDays = Math.floor(diffHours / 24);

        if (diffDays > 0) return `${diffDays}d`;
        if (diffHours > 0) return `${diffHours}h`;
        if (diffMins > 0) return `${diffMins}m`;
        return 'Just now';
    }

    function formatDuration(seconds) {
        if (seconds < 0.001) return `${(seconds * 1000000).toFixed(2)}us`;
        if (seconds < 1) return `${(seconds * 1000).toFixed(2)}ms`;
        return `${seconds.toFixed(2)}s`;
    }

    function jsonToYaml(obj, indent = 0) {
        const spaces = '  '.repeat(indent);
        let yaml = '';

        if (Array.isArray(obj)) {
            obj.forEach(item => {
                if (typeof item === 'object' && item !== null) {
                    yaml += spaces + '-\n' + jsonToYaml(item, indent + 1);
                } else {
                    yaml += spaces + '- ' + item + '\n';
                }
            });
        } else if (typeof obj === 'object' && obj !== null) {
            Object.keys(obj).forEach(key => {
                const value = obj[key];
                if (typeof value === 'object' && value !== null) {
                    yaml += spaces + key + ':\n' + jsonToYaml(value, indent + 1);
                } else {
                    yaml += spaces + key + ': ' + value + '\n';
                }
            });
        }

        return yaml;
    }

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
