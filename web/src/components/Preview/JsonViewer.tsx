/**
 * Files-tab JSON preview. The tree itself lives in `JsonTree` so the chat
 * transcript can import it statically while this module stays lazily loaded by
 * `PreviewSurface` (a module that is both dynamically and statically imported
 * would defeat the code split).
 */
export { default } from "./JsonTree";
