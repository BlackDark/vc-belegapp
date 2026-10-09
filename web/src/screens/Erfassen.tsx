import { A, useNavigate } from "@solidjs/router";
import { CaptureInputs, openCaptured } from "../components/CaptureInputs";

export default function Erfassen() {
  const navigate = useNavigate();
  return (
    <section class="flex flex-col gap-4">
      <h1 class="text-2xl font-semibold">Beleg erfassen</h1>
      <CaptureInputs onFile={(file) => void openCaptured(navigate, file)} />
      <A href="/belege/neu" class="text-center underline">
        Ohne Foto erfassen
      </A>
    </section>
  );
}
