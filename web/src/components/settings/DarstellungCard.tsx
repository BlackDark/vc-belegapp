import { applyTheme, currentTheme } from "../../lib/theme";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../ui/card";
import { Tabs, TabsList, TabsTrigger } from "../ui/tabs";

export function DarstellungCard() {
  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Darstellung</CardTitle>
        <CardDescription>
          Hell, Dunkel oder die Systemeinstellung.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Tabs
          value={currentTheme()}
          onChange={(value) => {
            if (value === "light" || value === "dark" || value === "system") {
              applyTheme(value);
            }
          }}
        >
          <TabsList aria-label="Darstellung">
            <TabsTrigger value="light">Hell</TabsTrigger>
            <TabsTrigger value="dark">Dunkel</TabsTrigger>
            <TabsTrigger value="system">System</TabsTrigger>
          </TabsList>
        </Tabs>
      </CardContent>
    </Card>
  );
}
