import { A, useNavigate } from "@solidjs/router";
import { client } from "../lib/api";
import { downscale } from "../lib/image";
import { CaptureInputs } from "./Heute";

export default function Erfassen() {
  const navigate = useNavigate();
  const upload = async (file: File) => {
    const blob = await downscale(file);
    const bild = await client.upload(blob);
    navigate(`/belege/neu?bild=${bild.id}`);
  };
  return (
    <section class="flex flex-col gap-4">
      <h1 class="text-2xl font-semibold">Beleg erfassen</h1>
      <CaptureInputs onFile={(file) => void upload(file)} />
      <A href="/belege/neu" class="text-center underline">
        Ohne Foto erfassen
      </A>
    </section>
  );
}
