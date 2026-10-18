import { ApiError } from "./api";

// The preview /api/v1/datenimport/pruefen returns before the user confirms.
// The token is what the confirm step posts back.
export type ImportPreview = {
  import_token: string;
  gueltig_bis: string;
  app_version: string;
  schema_version: number;
  zeitraum_von: string;
  zeitraum_bis: string;
  anzahl_belege: number;
};

// The outcome of a committed import.
export type ImportResult = {
  ok: boolean;
  anzahl_belege: number;
  zeitraum_von: string;
  zeitraum_bis: string;
  schema_version: number;
  app_version: string;
};

// uploadImport stages a backup ZIP through /api/v1/datenimport/pruefen. The
// request is an upload, so it goes through XMLHttpRequest to report progress;
// fetch cannot observe the upload progress of a large archive.
export function uploadImport(
  file: File,
  onProgress: (percent: number) => void,
): Promise<ImportPreview> {
  return new Promise((resolve, reject) => {
    const data = new FormData();
    data.set("datei", file);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1/datenimport/pruefen");
    xhr.withCredentials = true;
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        onProgress(Math.round((event.loaded / event.total) * 100));
      }
    };
    xhr.onload = () => {
      let body: (ImportPreview & { detail?: string; code?: string }) | null =
        null;
      if (xhr.responseText) {
        try {
          body = JSON.parse(xhr.responseText) as ImportPreview & {
            detail?: string;
            code?: string;
          };
        } catch {
          reject(
            new ApiError(xhr.status, { detail: "Die Antwort war kein JSON." }),
          );
          return;
        }
      }
      if (xhr.status >= 200 && xhr.status < 300 && body) {
        resolve(body);
        return;
      }
      reject(
        new ApiError(xhr.status, {
          detail: body?.detail,
          code: body?.code,
        }),
      );
    };
    xhr.onerror = () => reject(new Error("Upload fehlgeschlagen"));
    xhr.send(data);
  });
}
