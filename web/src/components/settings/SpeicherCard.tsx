import { Card, CardDescription, CardHeader, CardTitle } from "../ui/card";

export function SpeicherCard(props: { backend?: string }) {
  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Speicher</CardTitle>
        <CardDescription>{props.backend}</CardDescription>
      </CardHeader>
    </Card>
  );
}
