import { randomUUID } from "node:crypto";
import { open, mkdir, readFile, rename, rm } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

export type CursorStore = {
  load(): Promise<string | undefined>;
  save(cursor: string): Promise<void>;
};

type CursorState = {
  version: 1;
  workspaceId: string;
  cursor: string;
};

export class FileCursorStore implements CursorStore {
  private readonly path: string;
  private readonly workspaceId: string;

  constructor(path: string, workspaceId: string) {
    this.path = path;
    this.workspaceId = workspaceId;
  }

  async load(): Promise<string | undefined> {
    let serialized: string;
    try {
      serialized = await readFile(this.path, "utf8");
    } catch (error) {
      if (isNodeError(error) && error.code === "ENOENT") return undefined;
      throw error;
    }

    let value: unknown;
    try {
      value = JSON.parse(serialized);
    } catch (error) {
      throw new Error(`Invalid ClickClack cursor state at ${this.path}`, { cause: error });
    }
    if (!isCursorState(value)) {
      throw new Error(`Invalid ClickClack cursor state at ${this.path}`);
    }
    if (value.workspaceId !== this.workspaceId) {
      throw new Error(
        `ClickClack cursor state belongs to workspace ${value.workspaceId}, not ${this.workspaceId}`,
      );
    }
    return value.cursor;
  }

  async save(cursor: string): Promise<void> {
    const directory = dirname(this.path);
    await mkdir(directory, { recursive: true, mode: 0o700 });
    const temporary = join(directory, `.${basename(this.path)}.${process.pid}.${randomUUID()}.tmp`);
    let handle: Awaited<ReturnType<typeof open>> | undefined;
    try {
      handle = await open(temporary, "wx", 0o600);
      const state: CursorState = { version: 1, workspaceId: this.workspaceId, cursor };
      await handle.writeFile(`${JSON.stringify(state)}\n`, "utf8");
      await handle.sync();
      await handle.close();
      handle = undefined;
      await rename(temporary, this.path);

      const directoryHandle = await open(directory, "r");
      try {
        await directoryHandle.sync();
      } finally {
        await directoryHandle.close();
      }
    } catch (error) {
      await handle?.close().catch(() => undefined);
      await rm(temporary, { force: true }).catch(() => undefined);
      throw error;
    }
  }
}

function isCursorState(value: unknown): value is CursorState {
  if (!value || typeof value !== "object") return false;
  const state = value as Partial<CursorState>;
  return (
    state.version === 1 &&
    typeof state.workspaceId === "string" &&
    state.workspaceId.length > 0 &&
    typeof state.cursor === "string"
  );
}

function isNodeError(value: unknown): value is NodeJS.ErrnoException {
  return value instanceof Error && "code" in value;
}
