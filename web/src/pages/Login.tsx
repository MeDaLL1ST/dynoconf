import { Card, CardContent } from "@/components/ui";

export function Login() {
  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardContent className="flex flex-col items-center gap-6 p-8 text-center">
          <div className="grid h-12 w-12 place-items-center rounded-xl bg-primary text-xl font-bold text-primary-foreground">
            d
          </div>
          <div>
            <h1 className="text-xl font-semibold">dynoconf</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Runtime configuration for your services.
            </p>
          </div>
          <a
            href="/auth/login"
            className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-[#fc6d26] px-4 font-medium text-white transition-opacity hover:opacity-90"
          >
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="currentColor" aria-hidden>
              <path d="M12 21.5l3.7-11.4H8.3L12 21.5zM2.5 10.1l-1.1 3.4a.8.8 0 00.3.9l10.3 7.1-9.5-11.4zM2.5 10.1h5.8L5.8 2.4a.4.4 0 00-.76 0L2.5 10.1zm19 0l-1.1 3.4a.8.8 0 01-.3.9L9.8 21.5l9.5-11.4zm0 0h-5.8l2.5-7.7a.4.4 0 01.76 0l2.54 7.7z" />
            </svg>
            Sign in with GitLab
          </a>
          <p className="text-xs text-muted-foreground">
            Access is granted per service. Ask an admin if you don&apos;t see your
            service after signing in.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
