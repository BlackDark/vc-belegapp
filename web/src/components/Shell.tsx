import { A, useLocation, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import {
  CalendarDays,
  Camera,
  Monitor,
  Moon,
  Settings,
  Sun,
} from "lucide-solid";
import { type JSX, type ParentProps, Show, Suspense } from "solid-js";

import { ApiError, client } from "../lib/api";
import { queryKeys } from "../lib/queryKeys";
import { applyTheme, currentTheme } from "../lib/theme";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "./ui/dropdown-menu";
import { Separator } from "./ui/separator";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from "./ui/sidebar";
import { Skeleton } from "./ui/skeleton";

export default function Shell(props: ParentProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const me = useQuery(() => ({
    queryKey: queryKeys.me,
    queryFn: () => client.me(),
    retry: false,
  }));

  return (
    <Show
      when={
        !me.isError ||
        !(me.error instanceof ApiError) ||
        me.error.status !== 401
      }
      fallback={<Redirect to="/login" />}
    >
      <SidebarProvider>
        <Sidebar collapsible="icon">
          <SidebarHeader>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton size="lg" tooltip="Belegapp" as={A} href="/">
                  <span class="flex size-8 items-center justify-center rounded-lg bg-primary text-sm font-semibold text-primary-foreground">
                    B
                  </span>
                  <span class="grid text-left leading-tight">
                    <span class="truncate font-semibold">Belegapp</span>
                    <span class="truncate text-xs text-muted-foreground">
                      Essenszuschuss
                    </span>
                  </span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupLabel>Navigation</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  <NavItem
                    href="/"
                    label="Heute"
                    icon={<Sun />}
                    active={location.pathname === "/"}
                  />
                  <NavItem
                    href="/monat"
                    label="Monat"
                    icon={<CalendarDays />}
                    active={location.pathname.startsWith("/monat")}
                  />
                  <NavItem
                    href="/einstellungen"
                    label="Einstellungen"
                    icon={<Settings />}
                    active={location.pathname.startsWith("/einstellungen")}
                  />
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </SidebarContent>
          <SidebarFooter>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton
                  tooltip="Beleg erfassen"
                  onClick={() => navigate("/erfassen")}
                >
                  <Camera />
                  <span>Beleg erfassen</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <ThemeMenu />
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarFooter>
          <SidebarRail />
        </Sidebar>
        <SidebarInset>
          <header class="sticky top-0 z-20 flex h-14 items-center gap-2 border-b bg-background/80 px-3 backdrop-blur md:px-4">
            <SidebarTrigger />
            <Separator orientation="vertical" class="mr-1 h-4" />
            <span class="text-sm font-medium">Belegapp</span>
          </header>
          <div class="mx-auto flex w-full max-w-5xl flex-1 flex-col px-4 py-6 pb-28 md:px-8 md:pb-10">
            <Suspense fallback={<PageSkeleton />}>{props.children}</Suspense>
          </div>
        </SidebarInset>
        <nav class="fixed inset-x-0 bottom-0 z-30 border-t bg-background/95 px-4 py-2 backdrop-blur md:hidden">
          <div class="mx-auto grid max-w-lg grid-cols-3 items-end">
            <BottomLink
              href="/"
              label="Heute"
              icon={<Sun size={20} />}
              current={location.pathname === "/"}
            />
            <button
              type="button"
              class="mx-auto -mt-7 flex size-14 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              aria-label="Beleg erfassen"
              onClick={() => navigate("/erfassen")}
            >
              <Camera size={22} />
            </button>
            <div class="flex justify-around">
              <BottomLink
                href="/monat"
                label="Monat"
                icon={<CalendarDays size={20} />}
                current={location.pathname.startsWith("/monat")}
              />
              <BottomLink
                href="/einstellungen"
                label="Mehr"
                icon={<Settings size={20} />}
                current={location.pathname.startsWith("/einstellungen")}
              />
            </div>
          </div>
        </nav>
      </SidebarProvider>
    </Show>
  );
}

function NavItem(props: {
  href: string;
  label: string;
  icon: JSX.Element;
  active: boolean;
}) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        as={A}
        href={props.href}
        tooltip={props.label}
        isActive={props.active}
        aria-current={props.active ? "page" : undefined}
      >
        {props.icon}
        <span>{props.label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function BottomLink(props: {
  href: string;
  label: string;
  icon: JSX.Element;
  current: boolean;
}) {
  return (
    <A
      href={props.href}
      aria-current={props.current ? "page" : undefined}
      class={`flex flex-col items-center gap-1 py-2 text-xs ${props.current ? "text-foreground" : "text-muted-foreground"}`}
    >
      {props.icon}
      {props.label}
    </A>
  );
}

function ThemeMenu() {
  const current = () => currentTheme();
  const icon = () => {
    const theme = current();
    if (theme === "hell") return <Sun />;
    if (theme === "system") return <Monitor />;
    return <Moon />;
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        as={SidebarMenuButton}
        tooltip="Farbschema"
        aria-label="Farbschema"
      >
        {icon()}
        <span>Farbschema</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuRadioGroup
          value={current()}
          onChange={(value) => {
            if (value === "hell" || value === "dunkel" || value === "system") {
              applyTheme(value);
            }
          }}
        >
          <DropdownMenuRadioItem value="hell">Hell</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dunkel">Dunkel</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">System</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function PageSkeleton() {
  return (
    <div class="flex flex-col gap-4" aria-hidden="true">
      <Skeleton height={32} width={160} radius={8} />
      <Skeleton height={128} radius={12} />
      <div class="grid gap-3 sm:grid-cols-3">
        <Skeleton height={96} radius={12} />
        <Skeleton height={96} radius={12} />
        <Skeleton height={96} radius={12} />
      </div>
    </div>
  );
}

function Redirect(props: { to: string }) {
  const navigate = useNavigate();
  queueMicrotask(() => navigate(props.to, { replace: true }));
  return (
    <p class="p-6 text-sm text-muted-foreground">Weiter zur Anmeldung …</p>
  );
}
