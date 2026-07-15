"use strict";
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// src/index.ts
var index_exports = {};
__export(index_exports, {
  createHive: () => createHive
});
module.exports = __toCommonJS(index_exports);

// src/client.ts
var isObject = (val) => typeof val === "object" && val !== null;
var escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
function substituteParams(path, params) {
  if (!params) return path;
  let out = path;
  for (const [key, value] of Object.entries(params)) {
    if (value === void 0 || value === null) continue;
    const encoded = encodeURIComponent(String(value));
    out = out.replace(new RegExp(`\\{${escapeRe(key)}\\}`, "g"), encoded).replace(new RegExp(`:${escapeRe(key)}(?![a-zA-Z0-9_])`, "g"), encoded);
  }
  return out;
}
function appendQuery(url, query) {
  for (const [key, value] of Object.entries(query)) {
    if (value === void 0 || value === null) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item !== void 0 && item !== null) url.searchParams.append(key, String(item));
      }
    } else {
      url.searchParams.append(key, String(value));
    }
  }
}
var sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
function computeBackoff(attempt, limit) {
  const base = Math.min(limit, 300 * 2 ** (attempt - 1));
  return Math.round(Math.random() * base);
}
function parseRetryAfter(response, limit) {
  const header = response.headers.get("retry-after");
  if (!header) return void 0;
  let ms;
  if (/^\d+$/.test(header.trim())) {
    ms = Number(header) * 1e3;
  } else {
    const date = Date.parse(header);
    if (Number.isNaN(date)) return void 0;
    ms = date - Date.now();
  }
  if (ms < 0) ms = 0;
  return Math.min(ms, limit);
}
function combineSignals(signals) {
  const active = signals.filter((s) => !!s);
  if (active.length === 1) return active[0];
  if (typeof AbortSignal.any === "function") {
    return AbortSignal.any(active);
  }
  const controller = new AbortController();
  for (const s of active) {
    if (s.aborted) {
      controller.abort(s.reason);
      break;
    }
    s.addEventListener("abort", () => controller.abort(s.reason), { once: true });
  }
  return controller.signal;
}
var DEFAULT_RETRY_METHODS = ["get", "put", "head", "delete", "options", "trace"];
var DEFAULT_RETRY_STATUS = [408, 413, 429, 500, 502, 503, 504];
async function parseResponse(response, type) {
  if (type === "blob") return response.blob();
  if (type === "arrayBuffer") return response.arrayBuffer();
  if (type === "text") return response.text();
  const contentType = response.headers.get("content-type") || "";
  if (contentType.includes("application/json")) {
    return response.json();
  }
  return response.text();
}
var HTTPErrorImpl = class {
  constructor(status, response, message) {
    this.status = status;
    this.response = response;
    this.message = message;
    this.kind = "http";
  }
  isStatus(status) {
    return this.status === status;
  }
  isValidationError() {
    return this.status === 422;
  }
};
var NetworkErrorImpl = class {
  constructor(message) {
    this.message = message;
    this.kind = "network";
    this.status = void 0;
    this.response = void 0;
  }
  isStatus(_status) {
    return false;
  }
  isValidationError() {
    return false;
  }
};
function makeRequest(config, routeName, options) {
  const route = config.registry[routeName];
  if (!route) {
    throw new Error(`Route "${routeName}" not found in registry.`);
  }
  const opts = options || {};
  const urlPath = substituteParams(route.path, opts.params);
  const url = new URL(urlPath, config.baseUrl);
  if (opts.query) {
    appendQuery(url, opts.query);
  }
  const fetchInit = {
    method: route.method,
    headers: {
      ...config.headers,
      ...opts.fetchInit?.headers
    },
    ...opts.fetchInit
  };
  if (opts.body) {
    if (opts.body instanceof FormData || opts.body instanceof URLSearchParams || opts.body instanceof Blob) {
      fetchInit.body = opts.body;
    } else if (isObject(opts.body) && Object.values(opts.body).some((v) => v instanceof File || Array.isArray(v) && v[0] instanceof File)) {
      const formData = new FormData();
      for (const [key, value] of Object.entries(opts.body)) {
        if (value instanceof File) {
          formData.append(key, value);
        } else if (Array.isArray(value)) {
          value.forEach((item) => {
            if (item instanceof File) {
              formData.append(key, item);
            } else {
              formData.append(key, String(item));
            }
          });
        } else if (value !== void 0 && value !== null) {
          formData.append(key, String(value));
        }
      }
      fetchInit.body = formData;
    } else {
      fetchInit.body = JSON.stringify(opts.body);
      if (!fetchInit.headers) fetchInit.headers = {};
      fetchInit.headers["Content-Type"] = "application/json";
    }
  }
  if (config.credentials) {
    fetchInit.credentials = config.credentials;
  }
  const retry = { ...config.retry, ...opts.retry };
  const maxRetries = retry.limit ?? 0;
  const retryMethods = retry.methods ?? DEFAULT_RETRY_METHODS;
  const retryStatuses = retry.statusCodes ?? DEFAULT_RETRY_STATUS;
  const backoffLimit = retry.backoffLimit ?? 3e4;
  const methodRetryable = maxRetries > 0 && retryMethods.includes((route.method || "GET").toLowerCase());
  const timeout = opts.timeout ?? config.timeout ?? 3e4;
  const finalizeError = async (error) => {
    if (config.hooks?.beforeError) {
      for (const hook of config.hooks.beforeError) {
        error = await hook(error);
      }
    }
    return error;
  };
  const attempt = async (n) => {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);
    fetchInit.signal = combineSignals([controller.signal, opts.signal]);
    if (config.hooks?.beforeRequest) {
      const reqObj = new Request(url.toString(), { method: fetchInit.method || "GET" });
      for (const hook of config.hooks.beforeRequest) {
        await hook(reqObj);
      }
      const merged = new Headers(fetchInit.headers);
      reqObj.headers.forEach((v, k) => merged.set(k, v));
      fetchInit.headers = merged;
    }
    try {
      const response = await fetch(url.toString(), fetchInit);
      clearTimeout(timeoutId);
      if (config.hooks?.afterResponse) {
        const reqObj = new Request(url.toString(), { method: fetchInit.method || "GET" });
        for (const hook of config.hooks.afterResponse) {
          await hook(reqObj, response);
        }
      }
      if (!response.ok) {
        const errPayload = await parseResponse(response, opts.responseType);
        const error = new HTTPErrorImpl(
          response.status,
          errPayload,
          `Request failed with status ${response.status}`
        );
        const retryable = methodRetryable && n < maxRetries && retryStatuses.includes(response.status);
        const delay = parseRetryAfter(response, backoffLimit) ?? computeBackoff(n + 1, backoffLimit);
        return { retryable, error, delay };
      }
      return { data: await parseResponse(response, opts.responseType) };
    } catch (err) {
      clearTimeout(timeoutId);
      const error = new NetworkErrorImpl(err?.message || "Network error");
      return { retryable: methodRetryable && n < maxRetries, error, delay: computeBackoff(n + 1, backoffLimit) };
    }
  };
  const performFetch = async () => {
    for (let n = 0; ; n++) {
      const result = await attempt(n);
      if ("data" in result) return result.data;
      if (!result.retryable) throw await finalizeError(result.error);
      if (retry.onRetry) {
        await retry.onRetry({ attempt: n + 1, error: result.error, delay: result.delay });
      }
      await sleep(result.delay);
    }
  };
  const promise = performFetch();
  const hiveRequest = promise;
  hiveRequest.safe = async () => {
    try {
      const data = await promise;
      return [data, null];
    } catch (err) {
      return [null, err];
    }
  };
  return hiveRequest;
}
function createApiProxy(config, path = []) {
  return new Proxy(() => {
  }, {
    get(_target, prop) {
      if (typeof prop === "string") {
        return createApiProxy(config, [...path, prop]);
      }
      return void 0;
    },
    apply(_target, _thisArg, args) {
      const routeName = path.join(".");
      return makeRequest(config, routeName, args[0]);
    }
  });
}
function createHive(config) {
  const apiProxy = createApiProxy(config);
  return {
    api: apiProxy,
    $(name, options) {
      return makeRequest(config, name, options);
    },
    url(name, params, query) {
      const route = config.registry[name];
      if (!route) {
        throw new Error(`Route "${String(name)}" not found in registry.`);
      }
      const path = substituteParams(route.path, params);
      const url = new URL(path, config.baseUrl);
      if (query) {
        appendQuery(url, query);
      }
      return url.toString();
    },
    has(name) {
      return !!config.registry[name];
    },
    current(name, options) {
      if (typeof window === "undefined") return void 0;
      const pathname = window.location.pathname;
      let matchedRouteName;
      for (const [routeName, route] of Object.entries(config.registry)) {
        const regexStr = "^" + route.path.replace(/\{[a-zA-Z_][a-zA-Z0-9_]*\}/g, "[^/]+").replace(/:[a-zA-Z_][a-zA-Z0-9_]*/g, "[^/]+").replace(/\*/g, ".*") + "$";
        const regex = new RegExp(regexStr);
        if (regex.test(pathname)) {
          matchedRouteName = routeName;
          break;
        }
      }
      if (!name) {
        return matchedRouteName;
      }
      if (!matchedRouteName) {
        return false;
      }
      if (name.includes("*")) {
        const pattern = "^" + name.replace(/\*/g, ".*") + "$";
        return new RegExp(pattern).test(matchedRouteName);
      }
      return matchedRouteName === name;
    }
  };
}
// Annotate the CommonJS export names for ESM import in node:
0 && (module.exports = {
  createHive
});
