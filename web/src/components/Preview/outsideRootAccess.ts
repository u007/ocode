import { api } from "../../api/client";
import { isOutsideRootError } from "../../lib/outOfProjectFiles";

/**
 * Asks the server whether it would serve `path` to the preview under
 * `projectRoot`, without reading the file. Resolves true only when the server
 * refuses it as outside the project root (the case the user can grant).
 *
 * Any other refusal is logged and reported as false, so the caller keeps its
 * existing behaviour and the viewer shows its own error.
 */
export async function isOutsideProjectRoot(path: string, projectRoot?: string, host?: string): Promise<boolean> {
  try {
    await api.checkFileAccess(path, projectRoot, host);
    return false;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    if (isOutsideRootError(message)) {
      // Expected refusal, not a failure: the caller offers the grant instead.
      console.warn("File is outside the project root:", { path, projectRoot, host });
      return true;
    }
    console.error("Failed to check file access:", { path, projectRoot, host, err });
    return false;
  }
}
