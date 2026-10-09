import { A } from "@solidjs/router";
import type { JSX } from "solid-js";
import { For } from "solid-js";

import { cn } from "../../lib/utils";

export type CalendarDay = {
  key: string;
  label: string;
  href?: string;
  muted?: boolean;
  active?: boolean;
  today?: boolean;
};

// Month grid in the shadcn calendar shape. Days with an href are links.
export function Calendar(props: {
  caption: string;
  weekdays: string[];
  lead: number;
  days: CalendarDay[];
  renderDay?: (day: CalendarDay) => JSX.Element;
}) {
  return (
    <div class="rounded-xl border bg-card p-3 text-card-foreground">
      <div class="px-1 pb-2 text-sm font-medium">{props.caption}</div>
      <div class="grid grid-cols-7 gap-1 text-center text-xs">
        <For each={props.weekdays}>
          {(day) => (
            <span class="flex h-8 items-center justify-center text-muted-foreground">
              {day}
            </span>
          )}
        </For>
        <For each={Array.from({ length: props.lead })}>
          {() => <span class="h-9" />}
        </For>
        <For each={props.days}>
          {(day) =>
            props.renderDay ? props.renderDay(day) : <DayCell day={day} />
          }
        </For>
      </div>
    </div>
  );
}

function DayCell(props: { day: CalendarDay }) {
  const className = () =>
    cn(
      "flex h-9 items-center justify-center rounded-md text-sm hover:bg-accent hover:text-accent-foreground",
      props.day.muted && "bg-warning/50 text-warning-foreground",
      props.day.active &&
        "bg-primary font-semibold text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground",
      props.day.today && "ring-2 ring-ring",
    );
  return (
    <A href={props.day.href ?? "#"} class={className()}>
      {props.day.label}
    </A>
  );
}
