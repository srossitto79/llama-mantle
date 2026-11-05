<script lang="ts">
  import { connectionState } from "../stores/theme";

  let eventStatusColor = $derived.by(() => {
    switch ($connectionState) {
      case "connected":
        return "bg-success";
      case "connecting":
        return "bg-warning";
      case "disconnected":
      default:
        return "bg-destructive";
    }
  });

  let tooltipText = $derived(`Event Stream: ${$connectionState ?? "unknown"}`);
</script>

<div class="flex items-center" title={tooltipText}>
  <span class="inline-block w-3 h-3 rounded-full {eventStatusColor} mr-2"></span>
</div>
