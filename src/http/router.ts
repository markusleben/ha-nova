import type { IncomingMessage } from "node:http";

import { notFound } from "./errors.js";

export interface RouteContext {
  request: IncomingMessage;
  path: string;
  body: unknown;
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
