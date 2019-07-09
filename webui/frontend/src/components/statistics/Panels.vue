<template>
  <div class="container my-5">
    <app-panel
      title="More statistics about my tasks"
      :items="[
        {title: 'Completed tasks', value: '0'},
        {title: 'Average execution time', value: '0 milliseconds'},
        {title: 'Time operating', value: '0 seconds'},
        {title: 'State of the backup loop', value: 'inactive'}
      ]" 
    />
    <app-panel
      title="Raspberry statistics"
      :items="[
        {title: 'Temperature', value: '0ºC'},
        {title: 'CPU load', value: '0%'},
        {title: 'Free storage', value: '0GB'},
        {title: 'RAM usage', value: '0MB'}
      ]" 
    />
  </div>
</template>

<script>
import Panel from './Panel.vue'

export default {
  computed: {
    // TODO Set values from vuex
  },
  components: {
    appPanel: Panel
  }
}
</script>

<style lang="scss" scoped>
</style>
