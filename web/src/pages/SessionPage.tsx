import { useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useProjectState } from "../stores/projectStore";

/**
 * SessionPage is the deep-link entry point for /session/:id — including the
 * desktop `-session <id>` flag, which opens the webview at that route.
 *
 * Sessions now live as tabs on the Home view, so this page just opens the
 * session as a tab (deferring until the active project resolves if needed)
 * and redirects to "/". All message loading and tab-title behavior is owned
 * by SessionTabSync on the Home view.
 */
export default function SessionPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { openDeepLinkSession } = useProjectState();

  useEffect(() => {
    if (!id) return;
    openDeepLinkSession(id);
    navigate("/", { replace: true });
  }, [id, openDeepLinkSession, navigate]);

  return null;
}
