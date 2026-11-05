import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  type RouterHistory,
} from "@tanstack/react-router";
import { Zing } from "./Zing";
import { Privacy, Terms } from "./Legal";
import { RouteError } from "./ErrorBoundary";
export const rootRoute = createRootRoute({ component: Outlet });
const paths = [
  "/",
  "/login",
  "/register",
  "/verify",
  "/forgot",
  "/reset",
] as const;
const routeTree = rootRoute.addChildren([
  ...paths.map((path) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: Zing }),
  ),
  createRoute({
    getParentRoute: () => rootRoute,
    path: "/privacy",
    component: Privacy,
    staticData: { static: true },
  }),
  createRoute({
    getParentRoute: () => rootRoute,
    path: "/terms",
    component: Terms,
    staticData: { static: true },
  }),
]);
export function createAppRouter(history?: RouterHistory) {
  return createRouter({
    routeTree,
    defaultErrorComponent: RouteError,
    history,
  });
}
declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
  interface StaticDataRouteOption {
    static?: boolean;
    enhance?: string[];
  }
}
