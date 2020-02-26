<template>
  <v-container class="pa-6">
    <h2 class="text-center">My Tasks</h2>
    <v-row v-if="userTasks.length > 0" justify='center'>
      <v-col cols='10' xl='8'>

        <v-expansion-panels inset>

          <v-expansion-panel v-for="(userTask, i) in userTasks" :key="userTask.task.ID">
            <v-expansion-panel-header>
              {{ userTask.task.name }}
            </v-expansion-panel-header>
            <v-expansion-panel-content>
              <div class="d-flex justify-center">
                <v-card :elevation='0' outlined>
                  <v-card-text class="pa-1">
                    <v-btn
                      class="mx-4"
                      color='red darken-2'
                      @click="deleteTask(userTask.task.ID, i)"
                      text
                      icon
                    >
                      <v-icon>mdi-delete</v-icon>
                    </v-btn>
                    <v-btn
                      class="mx-4"
                      color='blue darken-2'
                      @click="editTask(userTask.task.ID)"
                      text
                      icon
                    >
                      <v-icon>mdi-pencil</v-icon>
                    </v-btn>
                    <v-btn
                      class="mx-4"
                      :color='
                        userTask.task.state === "Active" ? "green darken-2" : "red darken-2"
                      '
                      text
                      icon
                    >
                      <v-icon>mdi-power</v-icon>
                    </v-btn>
                  </v-card-text>
                </v-card>
              </div>
            </v-expansion-panel-content>
          </v-expansion-panel>

        </v-expansion-panels>

      </v-col>
    </v-row>

    <v-dialog v-model="showDialog" max-width='1000px' @click:outside='onDialogDismiss' scrollable>
      <router-view/>
    </v-dialog>

    <!-- <b-alert v-else variant="warning" class="m-4" fade>
      Oops... It seems that you have not created any task yet.
      Let's click on the "New" button to create a new one!
    </b-alert>
    <b-alert :show="err != ''" variant="danger" @dismissed="err = ''" class="floating-alert"  dismissible fade>
      <h5>Error when getting info</h5>
      <hr>
      <p>{{ err }}</p>
    </b-alert> -->
  </v-container>
</template>

<script>
import { mapGetters, mapActions } from 'vuex'

export default {
  data () {
    return {
      err: '',
      showDialog: false
    }
  },
  computed: {
    ...mapGetters('userTasks', {
      userTasks: 'tasks'
    })
  },
  methods: {
    ...mapActions('userTasks', [
      'removeUserTask'
    ]),
    editTask (taskID) {
      const targetRoute = '/management/edit'
      // Avoid pushing the current route.
      if (this.$route.path === targetRoute) return
      this.$router.push({ path: targetRoute, query: { id: taskID } })
      this.showDialog = true
    },
    deleteTask (taskID, index) {
      this.removeUserTask(taskID, index)
    },
    onDialogDismiss () {
      this.showDialog = false
      // To prevent cancellation of the animation
      setTimeout(() => {
        this.$router.replace({ name: 'management' })
      }, 400)
    }
  },
  components: {
  },
  beforeCreate () {
    // This need to be executed always because the user can create a task and later come here to
    // modify it, so, if the value is cached and no updated anymore, the new tasks won't be appear here.
    this.$store.dispatch('userTasks/fetchUserTasks')
  },
  mounted () {
    this.$root.$on('taskUpdated', () => {
      this.$store.dispatch('userTasks/fetchUserTasks')
      this.onDialogDismiss()
    })
  }
}
</script>

<style lang="scss" scoped>
</style>
