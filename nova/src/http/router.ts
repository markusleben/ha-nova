import type { IncomingMessage, ServerResponse } from "node:http";

import type { Principal } from "../security/principal.js";
import { notFound } from "./errors.js";

export interface RouteContext {
  request: IncomingMessage;
  response: ServerResponse;
  path: string;
  body: unknown;
  // Present only on listeners that resolve a principal (device/legacy functional
  // routes); absent on bearer-exempt and ingress routes.
  principal?: Principal;
}

export type RouteHandler = (context: RouteContext) => unknown | Promise<unknown>;

export interface Router {
  register(method: string, path: string, handler: RouteHandler): void;
  dispatch(method: string, path: string, context: RouteContext): Promise<unknown>;
}

interface RouteMap {
  [key: string]: RouteHandler | undefined;
}

export function createRouter(): Router {
  const routes: RouteMap = {};

  return {
    register(method, path, handler) {
      routes[makeRouteKey(method, path)] = handler;
    },
    async dispatch(method, path, context) {
      const route = routes[makeRouteKey(method, path)];
      if (!route) {
        throw notFound(method, path);
      }
      return await route(context);
    }
  };
}

function makeRouteKey(method: string, path: string): string {
  return `${method.toUpperCase()} ${path}`;
}
