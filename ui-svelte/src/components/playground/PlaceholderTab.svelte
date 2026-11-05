<script lang="ts">
  interface Props {
    featureName: string;
  }

  let { featureName }: Props = $props();
</script>

<div class="flex h-full items-center justify-center">
  <div class="text-muted-foreground text-center">
    <p class="text-sm font-medium">{featureName}</p>
    <p class="mt-2 text-sm">Not implemented</p>
  </div>
</div>
