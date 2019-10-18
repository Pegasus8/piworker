<template>
  <div class="container my-5">
    <app-panel
      title="More statistics about my tasks"
      :items="[
        { title: 'Completed tasks', value: completedTasksCounter },
        { title: 'Average execution time', value: averageExecutionTime + ' milliseconds' },
        { title: 'Time operating', value: runningTime + ' seconds' },
        { title: 'State of the backup loop', value: backupLoopState }
      ]"
    />
    <app-panel
      title="Raspberry statistics"
      :items="[
        { title: 'Temperature', value: raspberryStats.temperature + 'ºC' },
        { title: 'CPU load', value: raspberryStats.cpuLoad + '%' },
        { title: 'Free storage', value: raspberryStats.freeStorage },
        { title: 'RAM usage', value: raspberryStats.ramUsage }
      ]"
    />
  </div>
</template>

<script>
import Panel from './Panel.vue'
import { mapGetters } from 'vuex'

export default {
  computed: {
    ...mapGetters('statistics', [
      'completedTasksCounter',
      'averageExecutionTime',
      'runningTime',
      'backupLoopState',
      'raspberryStats'
    ])
  },
  components: {
    appPanel: Panel
  }
}
</script>

<style lang="scss" scoped>
</style>
