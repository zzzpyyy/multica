// Shared data of a workspace page, read by every column.
import { createContext, useContext } from "react";
import type { Issue, Workspace } from "../api";
import type { Ctx, Tree } from "../model";

export interface WorkspaceData extends Ctx {
  slug: string;
  workspace: Workspace;
  userId: string;
  tree: Tree;
  /** False until everything the issue columns read has loaded once (queries.ts pageState). */
  ready: boolean;
  /** The issue in the middle column, if any. */
  current: Issue | null;
}

export const WorkspaceContext = createContext<WorkspaceData | null>(null);

export function useWorkspace(): WorkspaceData {
  const v = useContext(WorkspaceContext);
  if (!v) throw new Error("useWorkspace outside a workspace");
  return v;
}
