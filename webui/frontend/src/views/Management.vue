<template>
  <v-container class="p-4">
    <h4 class="text-center">My Tasks</h4>
    <v-container v-if="userTasks.length > 0" fluid>
      <v-list nav>
        <v-list-item-group color='blue'>
          <v-list-item
            v-for="(userTask, i) in userTasks"
            :key="i"
            @click="editTask(userTask.task.name)"
          >
            <v-list-item-content>
              <v-list-item-title>
                {{ userTask.task.name }}
              </v-list-item-title>
            </v-list-item-content>
          </v-list-item>
        </v-list-item-group>
      </v-list>
    </v-container>

    <router-view/>

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
  methods: {
    editTask (taskName) {
      const targetRoute = '/management/task/' + taskName
      // Avoid pushing the current route.
      if (this.$route.path === targetRoute) return
      this.$router.push(targetRoute)
    }
  },
  components: {
  },
  beforeCreate () {
    // This need to be executed always because the user can create a task and later come here to
    // modify it, so, if the value is cached and no updated anymore, the new tasks won't be appear here.
    this.$store.dispatch('userTasks/fetchUserTasks')
  }
}
</script>

<style lang="scss" scoped>
</style>
