import { Route as rootRoute } from "./root";
import { Route as indexRoute } from "./index";

/** The route tree. Every route is registered here or it does not exist. */
export const routeTree = rootRoute.addChildren([indexRoute]);
