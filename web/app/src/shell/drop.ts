// Turning a drop into files (R6: every input works by drag and drop as
// well as by picker). A dropped folder arrives as a directory entry
// rather than its contents, so it has to be walked. The entry list is
// only valid inside the drop event, so dropEntries runs there and
// walkEntries runs after.

/** One dropped file and its path relative to the drop. */
export interface DroppedFile {
  file: File;
  path: string;
}

/**
 * Takes the drop's entries. Call this synchronously inside the drop
 * handler: the item list is empty by the time a promise resolves.
 * Returns an empty array where the entry API is absent, which leaves
 * the caller to fall back to dataTransfer.files.
 */
export function dropEntries(dt: DataTransfer): FileSystemEntry[] {
  // The DOM types promise items; engines outside N4's scope may not.
  const list = (dt as { items?: DataTransferItemList }).items;
  const items: DataTransferItem[] = list ? Array.from(list) : [];
  const out: FileSystemEntry[] = [];
  for (const item of items) {
    if (item.kind !== "file") continue;
    // Absent outside the browsers N4 scopes us to. Throwing here would
    // lose the drop inside the event handler, with nothing said.
    if (typeof item.webkitGetAsEntry !== "function") return [];
    const entry = item.webkitGetAsEntry();
    if (entry) out.push(entry);
  }
  return out;
}

/** Files whose name starts with a dot are the platform's, not the user's. */
function hidden(name: string): boolean {
  return name.startsWith(".");
}

function readFile(entry: FileSystemFileEntry): Promise<File> {
  return new Promise((resolve, reject) => {
    entry.file(resolve, reject);
  });
}

/**
 * readEntries hands back at most 100 entries per call and signals the
 * end with an empty batch, so a directory needs reading until it dries
 * up.
 */
async function readDir(entry: FileSystemDirectoryEntry): Promise<FileSystemEntry[]> {
  const reader = entry.createReader();
  const all: FileSystemEntry[] = [];
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => {
      reader.readEntries(resolve, reject);
    });
    if (batch.length === 0) return all;
    all.push(...batch);
  }
}

async function walk(entry: FileSystemEntry, prefix: string, out: DroppedFile[]): Promise<void> {
  if (hidden(entry.name)) return;
  if (entry.isFile) {
    const file = await readFile(entry as FileSystemFileEntry);
    out.push({ file, path: prefix + file.name });
    return;
  }
  if (!entry.isDirectory) return;
  const children = await readDir(entry as FileSystemDirectoryEntry);
  for (const child of children) {
    await walk(child, `${prefix}${entry.name}/`, out);
  }
}

/**
 * Walks the dropped entries depth first. Paths are relative to the
 * dropped folder, not to its parent, which is what an SFZ instrument
 * needs to resolve the samples it references.
 *
 * A dropped folder's own name is left out, because the folder picker
 * leaves it out too: webkitRelativePath reads "MyKit/kick.wav" and the
 * shell strips the first segment. The conversion pipeline reads the
 * root of the filesystem it is handed, so a leading segment would hide
 * every WAV in the folder and the import would report finding none.
 */
export async function walkEntries(entries: FileSystemEntry[]): Promise<DroppedFile[]> {
  const out: DroppedFile[] = [];
  for (const entry of entries) {
    if (hidden(entry.name)) continue;
    if (entry.isDirectory) {
      for (const child of await readDir(entry as FileSystemDirectoryEntry)) {
        await walk(child, "", out);
      }
      continue;
    }
    await walk(entry, "", out);
  }
  return out;
}
