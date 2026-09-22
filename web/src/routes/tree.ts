import { Route as rootRoute } from "./root";
import { Route as authenticatedRoute } from "./authenticated";
import { Route as indexRoute } from "./index";
import { Route as loginRoute } from "./login";
import {
  connectionsRoute,
  dashboardsRoute,
  peopleRoute,
  questionsRoute,
  settingsRoute,
} from "./placeholders";

/**
 * The route tree. Every route is registered here or it does not exist.
 *
 * `/login` hangs off the root; everything else hangs off the authenticated
 * layout, so a new page is behind the guard by default and letting one out
 * takes a deliberate edit. The other way round — a public tree with routes
 * opted into protection — means the next person to add a page has to remember,
 * and eventually does not.
 */
export const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([
    indexRoute,
    dashboardsRoute,
    questionsRoute,
    connectionsRoute,
    peopleRoute,
    settingsRoute,
  ]),
]);
