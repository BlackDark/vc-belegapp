import { createSignal } from "solid-js";
import { toast } from "solid-sonner";
import { client, fehlermeldung } from "../../lib/api";
import { LabeledField } from "../field";
import { Button } from "../ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../ui/card";

// Changing the password asks for the current one even though this browser is
// signed in: a session says who signed in once, not who is sitting here now,
// and somebody walking past an unlocked screen must not be able to lock the
// owner out. The server ends every other session afterwards.
export function PasswortCard() {
  const [aktuell, setAktuell] = createSignal("");
  const [neu, setNeu] = createSignal("");
  const [wiederholung, setWiederholung] = createSignal("");
  const [laeuft, setLaeuft] = createSignal(false);

  const aendern = async () => {
    if (neu() !== wiederholung()) {
      toast.error("Die neuen Passwörter stimmen nicht überein");
      return;
    }
    setLaeuft(true);
    try {
      await client.changePassword(aktuell(), neu());
      setAktuell("");
      setNeu("");
      setWiederholung("");
      toast.success("Passwort geändert");
    } catch (err) {
      toast.error(fehlermeldung(err, "Passwort konnte nicht geändert werden"));
    } finally {
      setLaeuft(false);
    }
  };

  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Passwort</CardTitle>
        <CardDescription>
          Das Passwort liegt als argon2id-Hash in der Datenbank. Andere
          Sitzungen werden bei einer Änderung beendet.
        </CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <LabeledField
          label="Aktuelles Passwort"
          type="password"
          autocomplete="current-password"
          value={aktuell()}
          onChange={setAktuell}
        />
        <LabeledField
          label="Neues Passwort"
          type="password"
          autocomplete="new-password"
          value={neu()}
          onChange={setNeu}
        />
        <LabeledField
          label="Neues Passwort wiederholen"
          type="password"
          autocomplete="new-password"
          value={wiederholung()}
          onChange={setWiederholung}
        />
        <Button
          variant="outline"
          class="self-start"
          disabled={laeuft()}
          onClick={() => void aendern()}
        >
          Passwort ändern
        </Button>
      </CardContent>
    </Card>
  );
}
