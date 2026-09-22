import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";

import { NetworkError } from "../api/client";
import { Alert } from "../ui/Alert";

/**
 * Tells the user when Pivot cannot reach the server.
 *
 * Two sources, because neither is sufficient on its own.
 *
 * `navigator.onLine` knows the machine has no network at all, instantly and
 * for free. What it does not know is whether *Pivot* is reachable: a laptop on
 * a café wifi that has stopped routing, and a server that is down, both report
 * `true` while nothing works. That is the common case, and the one a user is
 * most likely to mistake for the product being broken.
 *
 * So a request that never reached the server counts too. That is what
 * `NetworkError` means — the client distinguishes it from an error the server
 * sent, which is exactly the distinction this banner needs.
 */
export function OfflineBanner() {
  const { t } = useTranslation();

  const offline = useOffline();

  if (!offline) return null;

  return (
    <div className="p-3">
      {/*
        Not `live`. This appears while the user is reading something else, and
        a live region would interrupt them to announce a condition that will
        very often clear on its own. The page that owns a failed action is what
        announces it; this is ambient, and ambient is right.
      */}
      <Alert tone="warning" title={t("connection.offline")}>
        {t("connection.offlineDetail")}
      </Alert>
    </div>
  );
}

/**
 * Whether Pivot appears to be unreachable.
 *
 * Quick to say no: a query that succeeds clears its own error, so the banner
 * disappears the moment anything gets through. A stale warning about a problem
 * that has gone away trains people to ignore the next one.
 */
export function useOffline(): boolean {
  const client = useQueryClient();
  const cache = client.getQueryCache();

  const subscribe = useCallback(
    (onChange: () => void) => cache.subscribe(onChange),
    [cache],
  );

  const failedToReach = useSyncExternalStore(
    subscribe,
    () => cache.getAll().some((query) => query.state.error instanceof NetworkError),
    () => false,
  );

  return useBrowserOffline() || failedToReach;
}

/** `navigator.onLine`, watched. */
function useBrowserOffline(): boolean {
  const [offline, setOffline] = useState(() => navigatorOffline());

  useEffect(() => {
    function sync() {
      setOffline(navigatorOffline());
    }

    window.addEventListener("online", sync);
    window.addEventListener("offline", sync);

    return () => {
      window.removeEventListener("online", sync);
      window.removeEventListener("offline", sync);
    };
  }, []);

  return offline;
}

/** Defaults to online where `navigator.onLine` is not implemented. */
function navigatorOffline(): boolean {
  return typeof navigator !== "undefined" && navigator.onLine === false;
}
