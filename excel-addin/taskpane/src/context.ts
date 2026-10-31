import type { ExcelPort } from "./port";

/**
 * Volatile workbook context. It rides on the user message as a user-role block and never goes into
 * the system prompt or tool list (prompt-cache stability, see CLAUDE.md).
 */
export async function contextBlock(port: ExcelPort, workbookId: string, fileName: string): Promise<string> {
  const [active, sel, sheets] = await Promise.all([port.activeSheet(), port.selection(), port.listSheets()]);
  const used = sheets.find((s) => s.name === active)?.usedAddress ?? "empty";
  return [
    "[ocode:excel]",
    `workbook=${workbookId}`,
    `file=${fileName}`,
    `active_sheet=${active} used_range=${used}`,
    `selection=${sel.sheet}!${sel.address}`,
    `sheets=${sheets.filter((s) => s.visibility === "Visible").map((s) => s.name).join(", ")}`,
    "[/ocode:excel]",
  ].join("\n");
}

export const withContext = (block: string, text: string) => `${block}\n\n${text}`;
