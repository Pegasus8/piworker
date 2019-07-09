<template>
  <div>
    <app-boxes></app-boxes>
    <app-panels></app-panels>
  </div>
</template>

<script>
import Boxes from '../components/statistics/Boxes.vue'
import Panels from '../components/statistics/Panels.vue'

export default {
  components: {
    appBoxes: Boxes,
    appPanels: Panels
  }
}
</script>
