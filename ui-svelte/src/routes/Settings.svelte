<script lang="ts">
  import { connectionState, themeName, themeMode, themes, type ThemeMode, type ThemeName } from "../stores/theme";
  import { versionInfo } from "../stores/api";
  import * as Page from "$lib/components/ui/page/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Field from "$lib/components/ui/field/index.js";
  import { NativeSelect } from "$lib/components/ui/native-select/index.js";

  const modes: { value: ThemeMode; label: string }[] = [
    { value: "light", label: "Light" },
    { value: "dark", label: "Dark" },
    { value: "system", label: "System" },
  ];

  const buildInfo = $derived([
    { label: "Event stream", value: $connectionState ?? "unknown" },
    { label: "Version", value: $versionInfo?.version ?? "unknown" },
    { label: "Commit", value: $versionInfo?.commit?.substring(0, 7) ?? "unknown" },
    { label: "Build date", value: $versionInfo?.build_date ?? "unknown" },
  ]);
</script>

<Page.Root title="Settings" width="narrow">
  <Card.Root class="gap-0 py-0">
    <div class="bg-(--panel-header-bg) border-b px-4 py-2.5">
      <h2 class="text-heading">Appearance</h2>
    </div>
    <div class="space-y-4 p-4">
      <Field.Root label="Theme" id="settings-theme" layout="inline">
        {#snippet control({ id })}
          <NativeSelect {id} value={$themeName} onchange={(e) => themeName.set(e.currentTarget.value as ThemeName)}>
            {#each themes as theme (theme.value)}
              <option value={theme.value}>{theme.label}</option>
            {/each}
          </NativeSelect>
        {/snippet}
      </Field.Root>
      <Field.Root label="Mode" id="settings-mode" layout="inline">
        {#snippet control({ id })}
          <NativeSelect {id} value={$themeMode} onchange={(e) => themeMode.set(e.currentTarget.value as ThemeMode)}>
            {#each modes as mode (mode.value)}
              <option value={mode.value}>{mode.label}</option>
            {/each}
          </NativeSelect>
        {/snippet}
      </Field.Root>
    </div>
  </Card.Root>

  <Card.Root class="gap-0 py-0">
    <div class="bg-(--panel-header-bg) border-b px-4 py-2.5">
      <h2 class="text-heading">Build</h2>
    </div>
    <dl class="divide-y text-sm">
      {#each buildInfo as row (row.label)}
        <div class="flex justify-between gap-4 px-4 py-2.5">
          <dt class="text-muted-foreground">{row.label}</dt>
          <dd class="font-mono text-[0.8125rem]">{row.value}</dd>
        </div>
      {/each}
    </dl>
  </Card.Root>
</Page.Root>
