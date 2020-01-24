<template>
  <b-container class="p-4 text-center justify-content-center">
    <h4 class="text-light">My Tasks</h4>
    <b-container v-if="userTasks.length > 0" fluid>
      <app-task
        v-for="globalTaskInfo in userTasks" :key="(globalTaskInfo.task.name).replace(/\s/g, '_')"
        :taskName="globalTaskInfo.task.name"
        :taskState="globalTaskInfo.task.state"
        :triggers="[globalTaskInfo.task.trigger]"
        :actions="globalTaskInfo.task.actions"
        logs=""
      />
      <!-- TODO Logs integration -->
    </b-container>
    <b-alert v-else variant="warning" class="m-4" fade>
      Oops... It seems that you have not created any task yet.
      Let's click on the "New" button to create a new one!
    </b-alert>
    <b-alert :show="err != ''" variant="danger" @dismissed="err = ''" class="floating-alert"  dismissible fade>
      <h5>Error when getting info</h5>
      <hr>
      <p>{{ err }}</p>
    </b-alert>
  </b-container>
</template>

<script>
import Task from '../components/management/Task.vue'
import { mapGetters } from 'vuex'

export default {
  data () {
    return {
      err: ''
    }
  },
  computed: {
    ...mapGetters('userTasks', {
      userTasks: 'tasks'
    })
  },
  components: {
    appTask: Task
  },
  beforeCreate () {
    if (!this.$store.getters['elementsInfo/triggers'].length > 0) {
      this.$store.dispatch('elementsInfo/updateTriggersInfo')
        .catch((error) => {
          this.err = 'Error on trigger-structs API: ' + error
        })
    }
    if (!this.$store.getters['elementsInfo/actions'].length > 0) {
      this.$store.dispatch('elementsInfo/updateActionsInfo')
        .catch((error) => {
          this.err = 'Error on actions-structs API: ' + error
        })
    }
    this.$store.dispatch('userTasks/getUserTasks')
  }
}
</script>

<style lang="scss" scoped>
</style>
