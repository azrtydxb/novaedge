// NovaEdge Dashboard Application
(function() {
    'use strict';

    // API base URL
    const API_BASE = '/api/v1';

    // State
    let currentPage = 'dashboard';
    let currentNamespace = 'all';

    // Initialize the application
    function init() {
        setupNavigation();
        loadNamespaces();
        showPage('dashboard');

        // Auto-refresh every 30 seconds
        setInterval(refreshCurrentPage, 30000);
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

        // Load page content
        loadPageContent(page);
    }

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
                    await loadResources(container, 'gateways', 'ProxyGateway');
                    break;
                case 'routes':
                    await loadResources(container, 'routes', 'ProxyRoute');
                    break;
                case 'backends':
                    await loadResources(container, 'backends', 'ProxyBackend');
                    break;
                case 'vips':
                    await loadResources(container, 'vips', 'ProxyVIP');
                    break;
                case 'policies':
                    await loadResources(container, 'policies', 'ProxyPolicy');
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

        // Recent resources summary
        html += `
            <div class="card">
                <div class="card-header">
                    <h3>Quick Overview</h3>
                </div>
                <div class="card-body">
                    <p>Select a resource type from the sidebar to view and manage NovaEdge configurations.</p>
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

        if (resources.length === 0) {
            container.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon">&#128196;</div>
                    <h3>No ${resourceType} resources found</h3>
                    <p>Create a ${resourceType} resource to get started.</p>
                </div>
            `;
            return;
        }

        let html = `
            <div class="card">
                <div class="card-header">
                    <h3>${resourceType} Resources (${resources.length})</h3>
                </div>
                <div class="card-body">
                    <div class="table-container">
                        <table>
                            <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Namespace</th>
                                    <th>Age</th>
                                    <th>Status</th>
                                </tr>
                            </thead>
                            <tbody>
        `;

        resources.forEach(resource => {
            const metadata = resource.metadata || {};
            const status = resource.status || {};
            const conditions = status.conditions || [];
            const readyCondition = conditions.find(c => c.type === 'Ready' || c.type === 'Accepted');
            const isReady = readyCondition && readyCondition.status === 'True';

            html += `
                <tr class="clickable" onclick="showResourceDetail('${endpoint}', '${metadata.namespace}', '${metadata.name}')">
                    <td><strong>${escapeHtml(metadata.name || 'N/A')}</strong></td>
                    <td>${escapeHtml(metadata.namespace || 'N/A')}</td>
                    <td>${formatAge(metadata.creationTimestamp)}</td>
                    <td><span class="badge ${isReady ? 'badge-success' : 'badge-warning'}">${isReady ? 'Ready' : 'Pending'}</span></td>
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

    // Show resource detail
    window.showResourceDetail = async function(endpoint, namespace, name) {
        const container = document.getElementById('page-content');
        container.innerHTML = '<div class="loading"><div class="spinner"></div></div>';

        try {
            const resource = await fetchAPI(`/${endpoint}/${namespace}/${name}`);

            let html = `
                <div class="card">
                    <div class="card-header">
                        <h3>${escapeHtml(name)}</h3>
                        <button class="refresh-btn" onclick="showPage('${endpoint.replace(/s$/, '')}s')">Back to List</button>
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
                            <div class="code-block">${escapeHtml(JSON.stringify(resource.spec || {}, null, 2))}</div>
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
        const spec = resource.spec || {};
        const status = resource.status || {};

        let html = '<div class="detail-grid">';

        html += `
            <div class="detail-item">
                <div class="detail-label">Name</div>
                <div class="detail-value">${escapeHtml(metadata.name || 'N/A')}</div>
            </div>
            <div class="detail-item">
                <div class="detail-label">Namespace</div>
                <div class="detail-value">${escapeHtml(metadata.namespace || 'N/A')}</div>
            </div>
            <div class="detail-item">
                <div class="detail-label">UID</div>
                <div class="detail-value">${escapeHtml(metadata.uid || 'N/A')}</div>
            </div>
            <div class="detail-item">
                <div class="detail-label">Created</div>
                <div class="detail-value">${escapeHtml(metadata.creationTimestamp || 'N/A')}</div>
            </div>
        `;

        // Add spec-specific fields
        if (spec.vipRef) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">VIP Reference</div>
                    <div class="detail-value">${escapeHtml(spec.vipRef)}</div>
                </div>
            `;
        }

        if (spec.listeners) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Listeners</div>
                    <div class="detail-value">${spec.listeners.length} configured</div>
                </div>
            `;
        }

        if (spec.hostnames) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Hostnames</div>
                    <div class="detail-value">${escapeHtml(spec.hostnames.join(', ') || 'N/A')}</div>
                </div>
            `;
        }

        if (spec.rules) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Rules</div>
                    <div class="detail-value">${spec.rules.length} configured</div>
                </div>
            `;
        }

        if (spec.serviceRef) {
            html += `
                <div class="detail-item">
                    <div class="detail-label">Service</div>
                    <div class="detail-value">${escapeHtml(spec.serviceRef.name || 'N/A')}</div>
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
        const agents = await fetchAPI('/agents');

        if (agents.length === 0) {
            container.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon">&#9679;</div>
                    <h3>No agents found</h3>
                    <p>NovaEdge agents run as a DaemonSet on cluster nodes.</p>
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
    }

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
        // Simple JSON to YAML-like conversion
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
