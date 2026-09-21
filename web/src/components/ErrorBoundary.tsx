import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * The last line of defense.
 *
 * Without one, a render error in any component blanks the entire page — React
 * unmounts the tree and the user is left looking at white. A boundary turns
 * that into something recoverable, and something that can be reported.
 *
 * Still a class component: React has no hook equivalent of
 * componentDidCatch.
 */
export class ErrorBoundary extends Component<Props, State> {
  override state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    // Part 14 wires this to the error reporter. Logging it here means a
    // developer still sees it in the console until then.
    console.error("Unhandled render error", error, info.componentStack);
  }

  override render(): ReactNode {
    const { error } = this.state;

    if (error === null) return this.props.children;

    return (
      <main className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-4 p-6 text-center">
        <h1 className="text-2xl font-semibold">Something went wrong</h1>
        <p className="text-content-muted">
          Pivot hit an error it could not recover from. Reloading usually helps.
        </p>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="rounded-token bg-accent px-4 py-2 font-medium text-accent-content hover:bg-accent-hover"
        >
          Reload
        </button>
        <pre className="max-w-full overflow-auto rounded-token bg-surface-sunken p-3 text-left font-mono text-xs text-content-subtle">
          {error.message}
        </pre>
      </main>
    );
  }
}
