import mammoth from "mammoth";

type DocxWorkerRequest = { bytes: ArrayBuffer; htmlLimit: number };
type DocxWorkerResponse = { html: string } | { error: string };

const workerScope = globalThis as unknown as {
  onmessage: ((event: MessageEvent<DocxWorkerRequest>) => void) | null;
  postMessage: (message: DocxWorkerResponse) => void;
};

workerScope.onmessage = async ({ data }) => {
  try {
    const result = await mammoth.convertToHtml(
      { arrayBuffer: data.bytes },
      { convertImage: mammoth.images.dataUri, externalFileAccess: false },
    );
    if (new TextEncoder().encode(result.value).byteLength > data.htmlLimit) {
      workerScope.postMessage({
        error:
          "The converted DOCX is too large to preview safely. Download the original to open it locally.",
      });
      return;
    }
    workerScope.postMessage({ html: result.value });
  } catch {
    workerScope.postMessage({ error: "Could not convert this DOCX." });
  }
};
