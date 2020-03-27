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
  }/*,
  created () {
    const socket = new WebSocket(`ws://${location.host}/ws`)

    socket.onopen = () => {
      console.info('Websocket connection established')
    }

    socket.onerror = (error) => {
      console.error(error)
    }

    socket.onmessage = (event) => {
      // Handle JSON data received from the server
      const data = JSON.parse(event.data)

      // Vuex usage
      this.$store.dispatch('statistics/setActiveTasksCounter', data.activeTasks)
      this.$store.dispatch('statistics/setOnExecutionTasksCounter', data.onExecutionTasks)
      this.$store.dispatch('statistics/setInactiveTasksCounter', data.inactiveTasks)
      this.$store.dispatch('statistics/setCompletedTasksCounter', data.completedTasks)

      this.$store.dispatch('statistics/setAverageExecutionTime', data.averageExecutionTime)
      this.$store.dispatch('statistics/setRunningTime', data.operatingTime)
      this.$store.dispatch('statistics/setBackupLoopState', data.backupLoopState)

      this.$store.dispatch('statistics/setRaspberryStatistics', data.raspberryStats)
    }

    socket.onclose = (event) => {
     console.info("Websocket connection closed")
    }
  } */
}
</script>
