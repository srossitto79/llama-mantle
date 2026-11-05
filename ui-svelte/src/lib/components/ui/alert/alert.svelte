<script lang="ts" module>
  import { tv, type VariantProps } from "tailwind-variants";

  // Variants are roles, not colours. "info" is the neutral default for normal
  // behaviour; "destructive" is for failures only.
  export const alertVariants = tv({
    base: "relative grid w-full grid-cols-[auto_1fr] items-start gap-x-2.5 gap-y-0.5 rounded-(--panel-radius) border px-3 py-2.5 text-sm [&>svg]:mt-0.5 [&>svg]:size-4 has-[>svg]:grid-cols-[auto_1fr] [&>:not(svg)]:col-start-2",
    variants: {
      variant: {
        info: "bg-card text-card-foreground [&>svg]:text-muted-foreground",
        success: "border-success/40 bg-success/8 [&>svg]:text-success",
        warning: "border-warning/50 bg-warning/10 [&>svg]:text-warning",
        destructive: "border-destructive/40 bg-destructive/8 text-foreground [&>svg]:text-destructive",
      },
    },
    defaultVariants: { variant: "info" },
  });

  export type AlertVariant = VariantProps<typeof alertVariants>["variant"];
</script>

<script lang="ts">
  import type { HTMLAttributes } from "svelte/elements";
  import { cn, type WithElementRef } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    variant = "info",
    class: className,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & { variant?: AlertVariant } = $props();
</script>

<div
  bind:this={ref}
  data-slot="alert"
  role={variant === "destructive" ? "alert" : "status"}
  class={cn(alertVariants({ variant }), className)}
  {...restProps}
>
  {@render children?.()}
</div>
