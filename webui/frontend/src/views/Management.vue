<template>
  <div class="container p-4 text-center justify-content-center">
    <h4 class="text-light">My Tasks</h4>
    <div v-if="userTasks.length > 0" class="container-fluid">
      <app-task
        v-for="task in userTasks" :key="(task.Name).replace(/\s/g, '_')"
        :taskName="task.Name"
        :taskState="task.State"
        :triggers="[task.Trigger]"
        :actions="task.Actions"
        logs="" 
      />
      <!-- TODO Logs integration -->
    </div>
    <div v-else class="alert alert-warning m-4" role="alert">
      Oops... It seems that you have not created any task yet.
      Let's click on the "New" button to create a new one!
    </div>
  </div>
</template>

<script>
import Task from '../components/management/Task.vue'
export default {
  data() {
    return {
      userTasks: []
    }
  },
  components: {
    appTask: Task
  },
  created () {
    // TODO APIs requests
  },
  beforeCreate () {
    if (!this.$store.getters['elementsInfo/triggers'].length > 0) {
      this.$store.dispatch('elementsInfo/updateTriggersInfo')
    }
    if (!this.$store.getters['elementsInfo/actions'].length > 0) {
      this.$store.dispatch('elementsInfo/updateActionsInfo')
    }
  },
}
</script>

<style lang="scss" scoped>
</style>