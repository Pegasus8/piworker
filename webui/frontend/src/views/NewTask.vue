<template>
<v-row justify='center' :no-gutters='$route.query.id'> <!-- If the current view is NewTask, add gutters, otherwise, don't. -->
  <v-col :cols='!$route.query.id ? 10 : false' :lg='!$route.query.id ? 8 : false'>
    <v-card>
      <v-card-title>
        {{ $route.query.id ? `Edit task "${ this.taskName }"` : 'Create a new task' }}
      </v-card-title>
      <v-card-text>
        <v-form class="m-2">
          <v-text-field
            v-model="taskName"
            label="Task name"
            outlined
            required
          />

          <v-row justify='center'>
            <v-col cols='12' lg='6' align='center'>
              <app-elements-list
                card-title='Trigger'
                :user-elements="$store.getters['newTask/triggerSelected']"
                :dragAndDrop="!dragAndDropTriggers"
                @modified="setTrigger($event)"
                @remove-item='removeTrigger($event)'
                @open-selector='showTriggerSelectorDialog = true'
              />
              <v-checkbox v-model='dragAndDropTriggers' label='Drag & drop' color='blue'/>
            </v-col>

            <v-col cols='12' lg='6' align='center'>
              <app-elements-list
                card-title='Actions'
                :user-elements="$store.getters['newTask/actionsSelected']"
                :dragAndDrop="!dragAndDropActions"
                @modified="setActions($event)"
                @remove-item='removeAction($event)'
                @open-selector='showActionSelectorDialog = true'
                @order-modified='updateActionsOrder()'
              />
              <v-checkbox v-model='dragAndDropActions' label='Drag & drop' color='blue'/>
            </v-col>
          </v-row>

          <v-row justify='center'>
            <v-col cols='auto'>
              <v-switch
                v-model="stateSelected"
                color='success'
                label='Enabled'
                @change="setTaskState(stateSelected)"
                inset
              />
            </v-col>
          </v-row>
        </v-form>
      </v-card-text>
      <v-card-actions>
        <v-btn
          class="primary darken-2 my-1"
          :disabled="!isAllSelected"
          :loading='submitted'
          @click="submitTask()"
          block
        >
          {{$route.query.id ? 'Update' : 'Save'}}
        </v-btn>
      </v-card-actions>
    </v-card>

    <v-snackbar
      v-model="alert"
      :bottom='true'
      :color='alertVariant'
      :timeout='8000'
    >
      {{ responseContent }}
      <v-btn
        dark
        text
        @click="alert = false"
      >
        Close
      </v-btn>
    </v-snackbar>

    <app-element-selector
      elementType='trigger'
      :elements="triggers"
      :show='showTriggerSelectorDialog'
      @elementSelected='setTrigger($event)'
      @dismissed='showTriggerSelectorDialog = false'
    />
    <app-element-selector
      elementType='action'
      :elements="actions"
      :show='showActionSelectorDialog'
      @elementSelected='addAction($event)'
      @dismissed='showActionSelectorDialog = false'
    />
  </v-col>
</v-row>
</template>

<script>
import ElementSelector from '../components/new-task/ElementSelector.vue'
import ElementsList from '../components/new-task/ElementsList.vue'
import { mapGetters, mapMutations, mapActions } from 'vuex'

export default {
  data () {
    return {
      // taskNameRules: [
      //   v => !!v || 'The task must have a name'
      //   // TODO Check if the name of the task is not repeated.
      // ],
      showTriggerSelectorDialog: false,
      showActionSelectorDialog: false,

      newTrigger: '',
      newAction: '',
      stateSelected: true,
      submitted: false,
      alert: false,
      alertVariant: 'success',
      responseContent: '',
      dragAndDropTriggers: true,
      dragAndDropActions: true
    }
  },
  computed: {
    ...mapGetters('elementsInfo', [
      'triggers',
      'actions'
    ]),
    taskName: {
      get () {
        return this.$store.getters['newTask/taskname']
      },
      set (newValue) {
        return this.$store.commit('newTask/setTaskname', newValue)
      }
    },
    isAllSelected () {
      if (this.taskName && this.$store.getters['newTask/taskState'] !== '' &&
        this.$store.getters['newTask/triggerSelected'].length > 0 &&
        this.$store.getters['newTask/actionsSelected'].length > 0) {
        return true
      } else {
        return false
      }
    }
  },
  methods: {
    ...mapMutations('newTask', [
      'setTaskname',
      'setTaskState'
    ]),
    ...mapActions('newTask', [
      'setTrigger',
      'removeTrigger',
      'setActions',
      'addAction',
      'removeAction',
      'updateActionsOrder'
    ]),
    clearFields () {
      this.taskName = ''
      this.stateSelected = true
      this.setTaskState('')
      this.newTrigger = ''
      this.newAction = ''
      this.setActions([])
      this.setTrigger(null)
    },
    submitTask () {
      this.submitted = true
      console.info('Submitting a new task to the API...')

      this.$store.dispatch(
        this.$route.query.id ? 'newTask/updateTask' : 'newTask/submitTask',
        this.$route.query.id
      )
        .then((_response) => {
          // Show a success alert
          this.alert = true
          this.alertVariant = 'success'
          this.responseContent = this.$route.query.id ? 'Data updated correctly!' : 'Data submitted correctly!'
          this.clearFields()
          this.submitted = false

          setTimeout(() => {
            this.alert = false
            this.responseContent = ''
            if (!this.$route.query.id) {
              this.$router.replace({ name: 'statistics' })
            } else {
              this.$root.$emit('taskUpdated')
            }
          }, 2000)
        })
        .catch((err) => {
          this.alert = true
          this.alertVariant = 'error'
          this.responseContent = err
          // Change the submitted variable only when the response is received
          this.submitted = false
        })
    }
  },
  mounted () {
    if (this.$route.query.id) {
      // Coming from `Management` view.

      const userTask = this.$store.getters['userTasks/tasks'].find(t => t.ID === this.$route.query.id)

      this.taskName = userTask.name
      this.setActions(userTask.actions)
      this.setTrigger(userTask.trigger)
      this.setTaskState(userTask.state)
      this.stateSelected = userTask.state === 'Active'
    } else {
      // New Task view
      this.clearFields()
    }
    // Usually the user will use the default status of the tasks, therefore, it must be set
    // beforehand. Otherwise the value will not be saved in vuex.
    this.setTaskState(this.stateSelected)
  },
  components: {
    appElementSelector: ElementSelector,
    appElementsList: ElementsList
  }
}

</script>

<style lang="scss" scoped>
</style>
