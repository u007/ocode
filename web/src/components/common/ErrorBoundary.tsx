import { Component, type ReactNode } from "react";
import { Button } from "@/components/ui/button";

interface Props {
  children: ReactNode;
  /** Optional custom fallback. May be a node or a render function receiving
   *  the caught error and a `reset` callback that re-attempts rendering the
   *  children. Defaults to a small inline error panel. */
  fallback?: ReactNode | ((error: Error | null, reset: () => void) => ReactNode);
  /** Short label for diagnostics (e.g. "project:/proj"). */
  scope?: string;
  /** When this value changes, the boundary clears any caught error and
   *  re-attempts rendering its children (without remounting them). */
  resetKey?: unknown;
  /** Whether the default fallback may offer a full page reload. Only the
   *  root boundary should set this; per-project failures must not suggest
   *  reloading the whole app. */
  allowReload?: boolean;
  onError?: (error: Error, info: React.ErrorInfo) => void;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, error: null };

  reset = () => {
    this.setState({ hasError: false, error: null });
  };

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error("ErrorBoundary caught:", this.props.scope ?? "", error, info.componentStack);
    this.props.onError?.(error, info);
  }

  componentDidUpdate(prevProps: Props) {
    if (prevProps.resetKey !== this.props.resetKey && this.state.hasError) {
      this.reset();
    }
  }

  render() {
    if (this.state.hasError) {
      const { fallback } = this.props;
      if (typeof fallback === "function") {
        return fallback(this.state.error, this.reset);
      }
      if (fallback !== undefined) {
        return fallback;
      }
      return (
        <div className="flex h-full w-full items-center justify-center bg-card text-foreground p-4">
          <div className="text-center max-w-md">
            <h1 className="text-xl font-bold mb-2">Something went wrong</h1>
            <p className="text-muted-foreground text-sm mb-4 break-words">
              {this.state.error?.message ?? "Unknown error"}
            </p>
            <div className="flex items-center justify-center gap-2">
              <Button type="button" onClick={this.reset}>
                Retry
              </Button>
              {this.props.allowReload && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => window.location.reload()}
                >
                  Reload page
                </Button>
              )}
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
