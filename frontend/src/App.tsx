import { cn } from "@/lib/utils"

export default function App() {
  return (
    <main className={cn("container mx-auto flex min-h-screen flex-col items-center justify-center gap-4 p-8")}>
      <h1 className="text-3xl font-bold tracking-tight">Engelsiz</h1>
      <p className="text-muted-foreground">Bilginin erişim engeli olmaz.</p>
      <p className="text-sm text-muted-foreground">
        Uygulama temeli hazır. İçerik sağlayıcıları bir sonraki change&apos;de eklenecek.
      </p>
    </main>
  )
}
