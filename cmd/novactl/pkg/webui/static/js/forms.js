// NovaEdge Dashboard - Form Handling
(function() {
    'use strict';

    // Form templates for each resource type
    const FormTemplates = {
        gateway: {
            title: 'Gateway',
            fields: [
                { name: 'name', label: 'Name', type: 'text', required: true, placeholder: 'my-gateway' },
                { name: 'namespace', label: 'Namespace', type: 'text', required: true, placeholder: 'default' },
                { name: 'listeners', label: 'Listeners', type: 'array', itemTemplate: {
                    name: { type: 'text', label: 'Name', required: true },
                    port: { type: 'number', label: 'Port', required: true, min: 1, max: 65535 },
                    protocol: { type: 'select', label: 'Protocol', options: ['HTTP', 'HTTPS', 'TCP', 'TLS'] },
                    hostnames: { type: 'text', label: 'Hostnames (comma-separated)' }
                }},
                { name: 'tracing.enabled', label: 'Enable Tracing', type: 'checkbox' },
                { name: 'tracing.samplingRate', label: 'Sampling Rate (%)', type: 'number', min: 0, max: 100, showIf: 'tracing.enabled' },
                { name: 'accessLog.enabled', label: 'Enable Access Log', type: 'checkbox' },
                { name: 'accessLog.format', label: 'Log Format', type: 'select', options: ['json', 'common', 'combined'], showIf: 'accessLog.enabled' }
            ]
        },
        route: {
            title: 'Route',
            fields: [
                { name: 'name', label: 'Name', type: 'text', required: true, placeholder: 'my-route' },
                { name: 'namespace', label: 'Namespace', type: 'text', required: true, placeholder: 'default' },
                { name: 'hostnames', label: 'Hostnames (comma-separated)', type: 'text', placeholder: 'example.com, *.example.com' },
                { name: 'matches', label: 'Match Rules', type: 'array', itemTemplate: {
                    'path.type': { type: 'select', label: 'Path Type', options: ['Exact', 'PathPrefix', 'RegularExpression'] },
                    'path.value': { type: 'text', label: 'Path Value', placeholder: '/api' },
                    method: { type: 'select', label: 'Method', options: ['', 'GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'] }
                }},
                { name: 'backendRefs', label: 'Backends', type: 'array', required: true, itemTemplate: {
                    name: { type: 'text', label: 'Backend Name', required: true },
                    weight: { type: 'number', label: 'Weight', min: 0, max: 100 }
                }},
                { name: 'timeout', label: 'Timeout', type: 'text', placeholder: '30s' }
            ]
        },
        backend: {
            title: 'Backend',
            fields: [
                { name: 'name', label: 'Name', type: 'text', required: true, placeholder: 'my-backend' },
                { name: 'namespace', label: 'Namespace', type: 'text', required: true, placeholder: 'default' },
                { name: 'lbPolicy', label: 'Load Balancing Policy', type: 'select', required: true,
                  options: ['RoundRobin', 'P2C', 'EWMA', 'RingHash', 'Maglev'] },
                { name: 'endpoints', label: 'Endpoints', type: 'array', required: true, itemTemplate: {
                    address: { type: 'text', label: 'Address (host:port)', required: true, placeholder: 'localhost:8080' },
                    weight: { type: 'number', label: 'Weight', min: 0 }
                }},
                { name: 'healthCheck.enabled', label: 'Enable Health Check', type: 'checkbox' },
                { name: 'healthCheck.protocol', label: 'Health Check Protocol', type: 'select', options: ['HTTP', 'TCP'], showIf: 'healthCheck.enabled' },
                { name: 'healthCheck.path', label: 'Health Check Path', type: 'text', placeholder: '/health', showIf: 'healthCheck.enabled' },
                { name: 'healthCheck.interval', label: 'Interval', type: 'text', placeholder: '10s', showIf: 'healthCheck.enabled' },
                { name: 'healthCheck.timeout', label: 'Timeout', type: 'text', placeholder: '5s', showIf: 'healthCheck.enabled' }
            ]
        },
        vip: {
            title: 'VIP',
            fields: [
                { name: 'name', label: 'Name', type: 'text', required: true, placeholder: 'my-vip' },
                { name: 'namespace', label: 'Namespace', type: 'text', required: true, placeholder: 'default' },
                { name: 'address', label: 'Address (IP/CIDR)', type: 'text', required: true, placeholder: '192.168.1.100/32' },
                { name: 'mode', label: 'Mode', type: 'select', required: true, options: ['L2', 'BGP', 'OSPF'] },
                { name: 'interface', label: 'Interface', type: 'text', placeholder: 'eth0', showIf: (data) => data.mode === 'L2' },
                { name: 'bgp.localAS', label: 'Local AS', type: 'number', showIf: (data) => data.mode === 'BGP' },
                { name: 'bgp.peerAS', label: 'Peer AS', type: 'number', showIf: (data) => data.mode === 'BGP' },
                { name: 'bgp.peerIP', label: 'Peer IP', type: 'text', showIf: (data) => data.mode === 'BGP' },
                { name: 'ospf.area', label: 'OSPF Area', type: 'text', showIf: (data) => data.mode === 'OSPF' }
            ]
        },
        policy: {
            title: 'Policy',
            fields: [
                { name: 'name', label: 'Name', type: 'text', required: true, placeholder: 'my-policy' },
                { name: 'namespace', label: 'Namespace', type: 'text', required: true, placeholder: 'default' },
                { name: 'type', label: 'Policy Type', type: 'select', required: true, options: ['RateLimit', 'CORS', 'IPFilter', 'JWT'] },
                // Rate limit fields
                { name: 'rateLimit.requestsPerSecond', label: 'Requests/Second', type: 'number', min: 1, showIf: (data) => data.type === 'RateLimit' },
                { name: 'rateLimit.burstSize', label: 'Burst Size', type: 'number', min: 1, showIf: (data) => data.type === 'RateLimit' },
                { name: 'rateLimit.key', label: 'Rate Limit Key', type: 'select', options: ['client_ip', 'header:X-User-ID'], showIf: (data) => data.type === 'RateLimit' },
                // CORS fields
                { name: 'cors.allowOrigins', label: 'Allowed Origins (comma-separated)', type: 'text', showIf: (data) => data.type === 'CORS' },
                { name: 'cors.allowMethods', label: 'Allowed Methods (comma-separated)', type: 'text', showIf: (data) => data.type === 'CORS' },
                { name: 'cors.allowHeaders', label: 'Allowed Headers (comma-separated)', type: 'text', showIf: (data) => data.type === 'CORS' },
                { name: 'cors.allowCredentials', label: 'Allow Credentials', type: 'checkbox', showIf: (data) => data.type === 'CORS' },
                // IP Filter fields
                { name: 'ipFilter.allowList', label: 'Allow List (comma-separated CIDRs)', type: 'text', showIf: (data) => data.type === 'IPFilter' },
                { name: 'ipFilter.denyList', label: 'Deny List (comma-separated CIDRs)', type: 'text', showIf: (data) => data.type === 'IPFilter' },
                // JWT fields
                { name: 'jwt.issuer', label: 'Issuer', type: 'text', showIf: (data) => data.type === 'JWT' },
                { name: 'jwt.jwksUri', label: 'JWKS URI', type: 'text', showIf: (data) => data.type === 'JWT' }
            ]
        }
    };

    // Current form state
    let currentFormData = {};
    let currentFormType = null;
    let isEditMode = false;

    // Open create form
    window.openCreateForm = function(resourceType) {
        currentFormType = resourceType;
        isEditMode = false;
        currentFormData = getDefaultFormData(resourceType);

        showFormModal(resourceType, 'Create ' + FormTemplates[resourceType].title);
    };

    // Open edit form
    window.openEditForm = function(resourceType, data) {
        currentFormType = resourceType;
        isEditMode = true;
        currentFormData = JSON.parse(JSON.stringify(data)); // Deep copy

        showFormModal(resourceType, 'Edit ' + FormTemplates[resourceType].title);
    };

    // Get default form data
    function getDefaultFormData(resourceType) {
        const template = FormTemplates[resourceType];
        const data = {};

        template.fields.forEach(field => {
            const parts = field.name.split('.');
            let obj = data;
            for (let i = 0; i < parts.length - 1; i++) {
                if (!obj[parts[i]]) obj[parts[i]] = {};
                obj = obj[parts[i]];
            }

            if (field.type === 'checkbox') {
                obj[parts[parts.length - 1]] = false;
            } else if (field.type === 'array') {
                obj[parts[parts.length - 1]] = [];
            } else if (field.type === 'number') {
                obj[parts[parts.length - 1]] = field.min || 0;
            } else {
                obj[parts[parts.length - 1]] = '';
            }
        });

        // Set defaults
        if (resourceType === 'backend') {
            data.lbPolicy = 'RoundRobin';
        }
        if (resourceType === 'vip') {
            data.mode = 'L2';
        }
        if (resourceType === 'policy') {
            data.type = 'RateLimit';
        }

        data.namespace = window.currentNamespace !== 'all' ? window.currentNamespace : 'default';

        return data;
    }

    // Show form modal
    function showFormModal(resourceType, title) {
        const template = FormTemplates[resourceType];

        let html = `
            <div class="modal-overlay" onclick="closeModal()">
                <div class="modal" onclick="event.stopPropagation()">
                    <div class="modal-header">
                        <h3>${escapeHtml(title)}</h3>
                        <button class="modal-close" onclick="closeModal()">&times;</button>
                    </div>
                    <form id="resource-form" onsubmit="submitForm(event)">
                        <div class="modal-body">
                            <div class="form-fields">
                                ${renderFormFields(template.fields, currentFormData)}
                            </div>
                        </div>
                        <div class="modal-footer">
                            <button type="button" class="btn btn-secondary" onclick="closeModal()">Cancel</button>
                            <button type="submit" class="btn btn-primary">${isEditMode ? 'Update' : 'Create'}</button>
                        </div>
                    </form>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', html);

        // Focus first input
        const firstInput = document.querySelector('#resource-form input:not([type="checkbox"]), #resource-form select');
        if (firstInput) firstInput.focus();
    }

    // Render form fields
    function renderFormFields(fields, data) {
        let html = '';

        fields.forEach(field => {
            // Check if field should be shown
            if (field.showIf) {
                if (typeof field.showIf === 'function') {
                    if (!field.showIf(data)) return;
                } else if (typeof field.showIf === 'string') {
                    if (!getNestedValue(data, field.showIf)) return;
                }
            }

            const value = getNestedValue(data, field.name) || '';

            html += `<div class="form-group" data-field="${field.name}">`;
            html += `<label class="form-label">${escapeHtml(field.label)}${field.required ? ' <span class="required">*</span>' : ''}</label>`;

            switch (field.type) {
                case 'text':
                    html += `<input type="text" class="form-input" name="${field.name}" value="${escapeHtml(value)}"
                             ${field.required ? 'required' : ''} ${field.placeholder ? `placeholder="${field.placeholder}"` : ''}>`;
                    break;

                case 'number':
                    html += `<input type="number" class="form-input" name="${field.name}" value="${value}"
                             ${field.required ? 'required' : ''} ${field.min !== undefined ? `min="${field.min}"` : ''}
                             ${field.max !== undefined ? `max="${field.max}"` : ''}>`;
                    break;

                case 'select':
                    html += `<select class="form-select" name="${field.name}" ${field.required ? 'required' : ''}
                             onchange="handleFieldChange('${field.name}', this.value)">`;
                    field.options.forEach(opt => {
                        html += `<option value="${opt}" ${value === opt ? 'selected' : ''}>${opt || '-- Select --'}</option>`;
                    });
                    html += '</select>';
                    break;

                case 'checkbox':
                    html += `<input type="checkbox" class="form-checkbox" name="${field.name}" ${value ? 'checked' : ''}
                             onchange="handleFieldChange('${field.name}', this.checked)">`;
                    break;

                case 'array':
                    html += renderArrayField(field, Array.isArray(value) ? value : []);
                    break;
            }

            html += '</div>';
        });

        return html;
    }

    // Render array field
    function renderArrayField(field, items) {
        let html = `<div class="array-field" data-array="${field.name}">`;
        html += '<div class="array-items">';

        items.forEach((item, index) => {
            html += renderArrayItem(field, item, index);
        });

        html += '</div>';
        html += `<button type="button" class="btn btn-sm btn-secondary" onclick="addArrayItem('${field.name}')">+ Add</button>`;
        html += '</div>';

        return html;
    }

    // Render array item
    function renderArrayItem(field, item, index) {
        let html = `<div class="array-item" data-index="${index}">`;

        Object.entries(field.itemTemplate).forEach(([key, config]) => {
            const value = getNestedValue(item, key) || '';
            html += `<div class="array-item-field">`;
            html += `<label class="form-label-small">${config.label}</label>`;

            switch (config.type) {
                case 'text':
                    html += `<input type="text" class="form-input-sm" data-array-field="${field.name}" data-index="${index}" data-key="${key}"
                             value="${escapeHtml(value)}" ${config.required ? 'required' : ''} ${config.placeholder ? `placeholder="${config.placeholder}"` : ''}>`;
                    break;
                case 'number':
                    html += `<input type="number" class="form-input-sm" data-array-field="${field.name}" data-index="${index}" data-key="${key}"
                             value="${value}" ${config.min !== undefined ? `min="${config.min}"` : ''} ${config.max !== undefined ? `max="${config.max}"` : ''}>`;
                    break;
                case 'select':
                    html += `<select class="form-select-sm" data-array-field="${field.name}" data-index="${index}" data-key="${key}">`;
                    config.options.forEach(opt => {
                        html += `<option value="${opt}" ${value === opt ? 'selected' : ''}>${opt || '-- Select --'}</option>`;
                    });
                    html += '</select>';
                    break;
            }

            html += '</div>';
        });

        html += `<button type="button" class="btn-icon btn-danger" onclick="removeArrayItem('${field.name}', ${index})">&times;</button>`;
        html += '</div>';

        return html;
    }

    // Add array item
    window.addArrayItem = function(fieldName) {
        const template = FormTemplates[currentFormType];
        const field = template.fields.find(f => f.name === fieldName);
        if (!field) return;

        const newItem = {};
        Object.entries(field.itemTemplate).forEach(([key, config]) => {
            setNestedValue(newItem, key, config.type === 'number' ? 0 : '');
        });

        let arr = getNestedValue(currentFormData, fieldName);
        if (!Array.isArray(arr)) arr = [];
        arr.push(newItem);
        setNestedValue(currentFormData, fieldName, arr);

        // Re-render the array field
        const container = document.querySelector(`[data-array="${fieldName}"] .array-items`);
        if (container) {
            container.innerHTML = arr.map((item, index) => renderArrayItem(field, item, index)).join('');
        }
    };

    // Remove array item
    window.removeArrayItem = function(fieldName, index) {
        let arr = getNestedValue(currentFormData, fieldName);
        if (!Array.isArray(arr)) return;

        arr.splice(index, 1);
        setNestedValue(currentFormData, fieldName, arr);

        // Re-render the form
        refreshFormFields();
    };

    // Handle field change
    window.handleFieldChange = function(fieldName, value) {
        setNestedValue(currentFormData, fieldName, value);
        refreshFormFields();
    };

    // Refresh form fields
    function refreshFormFields() {
        const template = FormTemplates[currentFormType];
        const container = document.querySelector('.form-fields');
        if (container) {
            container.innerHTML = renderFormFields(template.fields, currentFormData);
        }
    }

    // Submit form
    window.submitForm = async function(event) {
        event.preventDefault();

        // Collect form data
        const form = document.getElementById('resource-form');
        const formData = new FormData(form);

        // Update currentFormData with form values
        formData.forEach((value, key) => {
            setNestedValue(currentFormData, key, value);
        });

        // Also update array field values
        document.querySelectorAll('[data-array-field]').forEach(input => {
            const fieldName = input.dataset.arrayField;
            const index = parseInt(input.dataset.index);
            const key = input.dataset.key;

            let arr = getNestedValue(currentFormData, fieldName);
            if (!Array.isArray(arr)) arr = [];
            if (!arr[index]) arr[index] = {};

            let value = input.type === 'number' ? parseInt(input.value) || 0 : input.value;
            setNestedValue(arr[index], key, value);
            setNestedValue(currentFormData, fieldName, arr);
        });

        // Convert comma-separated strings to arrays where needed
        if (currentFormData.hostnames && typeof currentFormData.hostnames === 'string') {
            currentFormData.hostnames = currentFormData.hostnames.split(',').map(s => s.trim()).filter(s => s);
        }
        if (currentFormData.cors?.allowOrigins && typeof currentFormData.cors.allowOrigins === 'string') {
            currentFormData.cors.allowOrigins = currentFormData.cors.allowOrigins.split(',').map(s => s.trim()).filter(s => s);
        }
        if (currentFormData.cors?.allowMethods && typeof currentFormData.cors.allowMethods === 'string') {
            currentFormData.cors.allowMethods = currentFormData.cors.allowMethods.split(',').map(s => s.trim()).filter(s => s);
        }
        if (currentFormData.cors?.allowHeaders && typeof currentFormData.cors.allowHeaders === 'string') {
            currentFormData.cors.allowHeaders = currentFormData.cors.allowHeaders.split(',').map(s => s.trim()).filter(s => s);
        }
        if (currentFormData.ipFilter?.allowList && typeof currentFormData.ipFilter.allowList === 'string') {
            currentFormData.ipFilter.allowList = currentFormData.ipFilter.allowList.split(',').map(s => s.trim()).filter(s => s);
        }
        if (currentFormData.ipFilter?.denyList && typeof currentFormData.ipFilter.denyList === 'string') {
            currentFormData.ipFilter.denyList = currentFormData.ipFilter.denyList.split(',').map(s => s.trim()).filter(s => s);
        }

        // Validate
        const errors = window.validateResource(currentFormType, currentFormData);
        if (errors.length > 0) {
            showToast('error', 'Validation Error: ' + errors[0].message);
            return;
        }

        // Submit
        try {
            const endpoint = currentFormType + 's';
            let url = `/api/v1/${endpoint}`;
            let method = 'POST';

            if (isEditMode) {
                url = `/api/v1/${endpoint}/${currentFormData.namespace}/${currentFormData.name}`;
                method = 'PUT';
            }

            const response = await fetch(url, {
                method: method,
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(currentFormData)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Request failed');
            }

            showToast('success', `${FormTemplates[currentFormType].title} ${isEditMode ? 'updated' : 'created'} successfully`);
            closeModal();
            window.refreshCurrentPage();
        } catch (error) {
            showToast('error', error.message);
        }
    };

    // Close modal
    window.closeModal = function() {
        const modal = document.querySelector('.modal-overlay');
        if (modal) modal.remove();
    };

    // Delete resource
    window.deleteResource = async function(resourceType, namespace, name) {
        if (!confirm(`Are you sure you want to delete ${resourceType} "${name}"?`)) {
            return;
        }

        try {
            const endpoint = resourceType + 's';
            const response = await fetch(`/api/v1/${endpoint}/${namespace}/${name}`, {
                method: 'DELETE'
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Delete failed');
            }

            showToast('success', `${resourceType} deleted successfully`);
            window.refreshCurrentPage();
        } catch (error) {
            showToast('error', error.message);
        }
    };

    // Show toast notification
    window.showToast = function(type, message) {
        const existing = document.querySelector('.toast');
        if (existing) existing.remove();

        const toast = document.createElement('div');
        toast.className = `toast toast-${type}`;
        toast.innerHTML = `
            <span class="toast-message">${escapeHtml(message)}</span>
            <button class="toast-close" onclick="this.parentElement.remove()">&times;</button>
        `;

        document.body.appendChild(toast);

        setTimeout(() => {
            if (toast.parentElement) toast.remove();
        }, 5000);
    };

    // Utility functions
    function getNestedValue(obj, path) {
        return path.split('.').reduce((o, k) => (o || {})[k], obj);
    }

    function setNestedValue(obj, path, value) {
        const parts = path.split('.');
        let current = obj;
        for (let i = 0; i < parts.length - 1; i++) {
            if (!current[parts[i]]) current[parts[i]] = {};
            current = current[parts[i]];
        }
        current[parts[parts.length - 1]] = value;
    }

    function escapeHtml(str) {
        if (str === null || str === undefined) return '';
        const div = document.createElement('div');
        div.textContent = String(str);
        return div.innerHTML;
    }

})();
