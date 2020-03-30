<template>
  <div>
    <div class='d-flex flex-row-reverse'>
      <v-btn :to='{ name: "detailed-statistics" }' small text>
        Detailed statistics >>
      </v-btn>
    </div>
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
  },
  mounted () {
    this.$websocket.connect()
  }
}
</script>
