// Package browserpm provides a browser management module based on playwright-go.
package browserpm

// StealthScript is a comprehensive anti-detection script that masks browser automation.
// This script is executed before any page scripts to override detection vectors.
const StealthScript = `
// Override navigator.webdriver
Object.defineProperty(navigator, 'webdriver', {
    get: () => undefined,
    configurable: true
});

// Override navigator.plugins to appear as a real browser
Object.defineProperty(navigator, 'plugins', {
    get: () => {
        const plugins = {
            length: 3,
            0: { 
                name: 'Chrome PDF Plugin', 
                description: 'Portable Document Format', 
                filename: 'internal-pdf-viewer',
                length: 1
            },
            1: { 
                name: 'Chrome PDF Viewer', 
                description: '', 
                filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai',
                length: 1
            },
            2: { 
                name: 'Native Client', 
                description: '', 
                filename: 'internal-nacl-plugin',
                length: 2
            },
            item: function(i) { return this[i] || null; },
            namedItem: function(name) {
                for (let i = 0; i < this.length; i++) {
                    if (this[i].name === name) return this[i];
                }
                return null;
            },
            refresh: function() {}
        };
        // Make it look like PluginArray
        Object.setPrototypeOf(plugins, PluginArray.prototype);
        return plugins;
    },
    configurable: true
});

// Override navigator.languages
Object.defineProperty(navigator, 'languages', {
    get: () => ['zh-CN', 'zh', 'en-US', 'en'],
    configurable: true
});

// Override navigator.platform
Object.defineProperty(navigator, 'platform', {
    get: () => 'Win32',
    configurable: true
});

// Override navigator.hardwareConcurrency
Object.defineProperty(navigator, 'hardwareConcurrency', {
    get: () => 8,
    configurable: true
});

// Override navigator.deviceMemory
Object.defineProperty(navigator, 'deviceMemory', {
    get: () => 8,
    configurable: true
});

// Hide automation in permissions
const originalQuery = window.navigator.permissions?.query;
if (originalQuery) {
    window.navigator.permissions.query = (parameters) => (
        parameters.name === 'notifications' ?
            Promise.resolve({ state: Notification.permission }) :
            originalQuery.call(window.navigator.permissions, parameters)
    );
}

// Override chrome runtime to appear as real Chrome
if (!window.chrome) {
    window.chrome = {};
}
if (!window.chrome.runtime) {
    window.chrome.runtime = {
        connect: function() {},
        sendMessage: function() {},
        onMessage: { addListener: function() {} },
        onConnect: { addListener: function() {} }
    };
}
if (!window.chrome.csi) {
    window.chrome.csi = function() {};
}
if (!window.chrome.loadTimes) {
    window.chrome.loadTimes = function() {
        return {
            commitLoadTime: Date.now() / 1000 - Math.random() * 2,
            connectionInfo: 'http/1.1',
            finishDocumentLoadTime: Date.now() / 1000 - Math.random(),
            finishLoadTime: Date.now() / 1000,
            firstPaintAfterLoadTime: 0,
            firstPaintTime: Date.now() / 1000 - Math.random() * 3,
            navigationType: 'Other',
            npnNegotiatedProtocol: 'unknown',
            requestTime: Date.now() / 1000 - Math.random() * 4,
            startLoadTime: Date.now() / 1000 - Math.random() * 3,
            wasAlternateProtocolAvailable: false,
            wasFetchedViaSpdy: false,
            wasNpnNegotiated: false
        };
    };
}

// Override WebGL renderer to hide SwiftShader
const getParameter = WebGLRenderingContext.prototype.getParameter;
WebGLRenderingContext.prototype.getParameter = function(parameter) {
    if (parameter === 37445) {
        return 'Intel Inc.';
    }
    if (parameter === 37446) {
        return 'Intel Iris OpenGL Engine';
    }
    return getParameter.call(this, parameter);
};

// Also for WebGL2
if (typeof WebGL2RenderingContext !== 'undefined') {
    const getParameter2 = WebGL2RenderingContext.prototype.getParameter;
    WebGL2RenderingContext.prototype.getParameter = function(parameter) {
        if (parameter === 37445) {
            return 'Intel Inc.';
        }
        if (parameter === 37446) {
            return 'Intel Iris OpenGL Engine';
        }
        return getParameter2.call(this, parameter);
    };
}

// Hide automation detection via Function.toString
const oldToString = Function.prototype.toString;
Function.prototype.toString = function() {
    if (this === navigator.webdriver) {
        return 'function webdriver() { [native code] }';
    }
    return oldToString.call(this);
};

// Override navigator.permissions.query for notifications
const originalPermissionsQuery = navigator.permissions?.query;
if (originalPermissionsQuery) {
    navigator.permissions.query = function(parameters) {
        if (parameters.name === 'notifications') {
            return Promise.resolve({ 
                state: Notification.permission,
                onchange: null
            });
        }
        return originalPermissionsQuery.call(navigator.permissions, parameters);
    };
}

// Mock screen properties
Object.defineProperty(screen, 'colorDepth', {
    get: () => 24,
    configurable: true
});

Object.defineProperty(screen, 'pixelDepth', {
    get: () => 24,
    configurable: true
});

// Override navigator.maxTouchPoints
Object.defineProperty(navigator, 'maxTouchPoints', {
    get: () => 0,
    configurable: true
});

// Fix iframe contentWindow detection
const originalContentWindow = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, 'contentWindow');
Object.defineProperty(HTMLIFrameElement.prototype, 'contentWindow', {
    get: function() {
        const window = originalContentWindow.get.call(this);
        if (window) {
            try {
                Object.defineProperty(window.navigator, 'webdriver', {
                    get: () => undefined,
                    configurable: true
                });
            } catch (e) {}
        }
        return window;
    }
});

console.log('[Stealth] Anti-detection script loaded');
`

// StealthScriptLight is a lighter version that focuses on the most critical overrides.
const StealthScriptLight = `
// Override navigator.webdriver - most critical for detection
Object.defineProperty(navigator, 'webdriver', {
    get: () => undefined,
    configurable: true
});

// Override chrome runtime
if (!window.chrome) {
    window.chrome = { runtime: {} };
}

console.log('[Stealth-Light] Anti-detection script loaded');
`
