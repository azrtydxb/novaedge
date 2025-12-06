// NovaEdge Dashboard - Validation
(function() {
    'use strict';

    // Validation rules for each resource type
    const ValidationRules = {
        gateway: {
            name: [required(), validName()],
            namespace: [required(), validName()],
            listeners: [required(), minLength(1, 'At least one listener is required')],
            'listeners.*.name': [required()],
            'listeners.*.port': [required(), range(1, 65535)],
            'listeners.*.protocol': [required(), oneOf(['HTTP', 'HTTPS', 'TCP', 'TLS'])]
        },
        route: {
            name: [required(), validName()],
            namespace: [required(), validName()],
            backendRefs: [required(), minLength(1, 'At least one backend is required')],
            'backendRefs.*.name': [required()]
        },
        backend: {
            name: [required(), validName()],
            namespace: [required(), validName()],
            lbPolicy: [required(), oneOf(['RoundRobin', 'P2C', 'EWMA', 'RingHash', 'Maglev'])],
            endpoints: [required(), minLength(1, 'At least one endpoint is required')],
            'endpoints.*.address': [required(), validAddress()]
        },
        vip: {
            name: [required(), validName()],
            namespace: [required(), validName()],
            address: [required(), validCIDR()],
            mode: [required(), oneOf(['L2', 'BGP', 'OSPF'])]
        },
        policy: {
            name: [required(), validName()],
            namespace: [required(), validName()],
            type: [required(), oneOf(['RateLimit', 'CORS', 'IPFilter', 'JWT'])]
        }
    };

    // Validate a resource
    window.validateResource = function(resourceType, data) {
        const rules = ValidationRules[resourceType];
        if (!rules) return [];

        const errors = [];

        Object.entries(rules).forEach(([field, fieldRules]) => {
            if (field.includes('.*')) {
                // Array field validation
                const [arrayField, itemField] = field.split('.*.');
                const array = getNestedValue(data, arrayField);
                if (Array.isArray(array)) {
                    array.forEach((item, index) => {
                        const value = getNestedValue(item, itemField);
                        fieldRules.forEach(rule => {
                            const error = rule(value, `${arrayField}[${index}].${itemField}`, data);
                            if (error) errors.push({ field: `${arrayField}[${index}].${itemField}`, message: error });
                        });
                    });
                }
            } else {
                const value = getNestedValue(data, field);
                fieldRules.forEach(rule => {
                    const error = rule(value, field, data);
                    if (error) errors.push({ field, message: error });
                });
            }
        });

        return errors;
    };

    // Validation rule factories
    function required(message = 'This field is required') {
        return (value, field) => {
            if (value === undefined || value === null || value === '' ||
                (Array.isArray(value) && value.length === 0)) {
                return message;
            }
            return null;
        };
    }

    function validName(message = 'Must be a valid name (lowercase, alphanumeric, hyphens)') {
        return (value, field) => {
            if (!value) return null;
            if (!/^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$/.test(value)) {
                return message;
            }
            return null;
        };
    }

    function range(min, max, message) {
        return (value, field) => {
            if (value === undefined || value === null || value === '') return null;
            const num = parseInt(value);
            if (isNaN(num) || num < min || num > max) {
                return message || `Must be between ${min} and ${max}`;
            }
            return null;
        };
    }

    function minLength(min, message) {
        return (value, field) => {
            if (!Array.isArray(value) || value.length < min) {
                return message || `Must have at least ${min} items`;
            }
            return null;
        };
    }

    function oneOf(options, message) {
        return (value, field) => {
            if (!value) return null;
            if (!options.includes(value)) {
                return message || `Must be one of: ${options.join(', ')}`;
            }
            return null;
        };
    }

    function validAddress(message = 'Must be a valid host:port address') {
        return (value, field) => {
            if (!value) return null;
            // Simple check for host:port format
            if (!/^[a-zA-Z0-9.-]+:\d+$/.test(value)) {
                return message;
            }
            return null;
        };
    }

    function validCIDR(message = 'Must be a valid IP/CIDR (e.g., 192.168.1.100/32)') {
        return (value, field) => {
            if (!value) return null;
            // Simple CIDR validation
            if (!/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\/\d{1,2}$/.test(value)) {
                return message;
            }
            return null;
        };
    }

    function validDuration(message = 'Must be a valid duration (e.g., 30s, 5m)') {
        return (value, field) => {
            if (!value) return null;
            if (!/^\d+[smh]$/.test(value)) {
                return message;
            }
            return null;
        };
    }

    // Utility function
    function getNestedValue(obj, path) {
        return path.split('.').reduce((o, k) => (o || {})[k], obj);
    }

})();
