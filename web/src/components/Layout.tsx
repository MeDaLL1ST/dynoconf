import { Link, useLocation } from "react-router-dom";
import { Moon, Sun, Boxes, ScrollText, Shield, LogOut } from "lucide-react";
import { type Me } from "@/lib/api";
import { useTheme } from "@/lib/theme";
import { Badge, Button } from "./ui";
import { cn } from "@/lib/utils";

export function Layout({ me, children }: { me: Me; children: React.ReactNode }) {
  const { theme, toggle } = useTheme();
  const loc = useLocation();

  const nav = [
    { to: "/", label: "Services", icon: Boxes },
    ...(me.role === "admin"
      ? [
          { to: "/audit", label: "Audit", icon: ScrollText },
          { to: "/admin", label: "Admin", icon: Shield },
        ]
      : []),
  ];

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-border bg-background/80 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4">
          <Link to="/" className="flex items-center gap-2 font-semibold">
            <span className="grid h-7 w-7 place-items-center rounded-md bg-primary text-primary-foreground">
              d
            </span>
            dynoconf
          </Link>
          <Badge variant="outline" className="hidden sm:inline-flex">
            {me.contour}
          </Badge>

          <nav className="ml-4 flex items-center gap-1">
            {nav.map((n) => {
              const active =
                n.to === "/" ? loc.pathname === "/" : loc.pathname.startsWith(n.to);
              return (
                <Link
                  key={n.to}
                  to={n.to}
                  className={cn(
                    "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition-colors",
                    active
                      ? "bg-accent text-foreground"
                      : "text-muted-foreground hover:bg-accent/60"
                  )}
                >
                  <n.icon className="h-4 w-4" />
                  {n.label}
                </Link>
              );
            })}
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <Button variant="ghost" size="icon" onClick={toggle} title="Toggle theme">
              {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </Button>
            <div className="hidden text-right text-xs leading-tight sm:block">
              <div className="font-medium">{me.name || me.email}</div>
              <div className="text-muted-foreground">{me.role}</div>
            </div>
            <form action="/auth/logout" method="post">
              <Button variant="outline" size="icon" type="submit" title="Sign out">
                <LogOut className="h-4 w-4" />
              </Button>
            </form>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-4 py-8">{children}</main>
    </div>
  );
}
