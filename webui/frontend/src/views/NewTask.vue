<template>
<v-container>
  <v-card>
    <v-card-title>
      Create a new task
    </v-card-title>
    <v-card-text>
      <v-form class="m-2">
        <v-text-field
          v-model="taskName"
          :rules='taskNameRules'
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
  </v-card>

  <v-btn
    class="primary darken-2 m-1"
    :disabled="!isAllSelected"
    :loading='submitted'
    @click="submitTask()"
    block
  >
    {{$route.params.name ? 'Update' : 'Save'}}
  </v-btn>

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
</v-container>
</template>

<script>
import ElementSelector from '../components/new-task/ElementSelector.vue'
import ElementsList from '../components/new-task/ElementsList.vue'
import { mapMutations, mapGetters } from 'vuex'
import router from '../router'

export default {
  data () {
    return {
      taskNameRules: [
        v => !!v || 'The task must have a name'
        // TODO Check if the name of the task is not repeated.
      ],
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
      if (this.taskName && this.stateSelected &&
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
      'setTaskState',
      'setTrigger',
      'removeTrigger',
      'addAction',
      'removeAction',
      'setActions',
      'updateActionsOrder'
    ]),
    clearFields () {
      this.taskName = ''
      this.stateSelected = ''
      this.setTaskState('')
      this.newTrigger = ''
      this.newAction = ''
      this.setActions([])
      this.setTrigger(null)
    },
    submitTask () {
      this.submitted = true
      console.info('Submitting a new task to the API...')
      this.$store.dispatch('newTask/submitData')
        .then((response) => {
          if (response.data.successful) {
            // Show a success alert
            this.alert = true
            this.alertVariant = 'success'
            this.responseContent = 'Data submitted correctly!'
            this.clearFields()
            setTimeout(() => {
              this.alert = false
              this.responseContent = ''
              router.replace({ name: 'statistics' })
            }, 2000)
          } else {
            // Show an error alert, showing the message received (response.data.error)
            this.alert = true
            this.alertVariant = 'error'
            this.responseContent = response.data.error
          }
          // Change the submitted variable only when the response is received
          this.submitted = false
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
    // Usually the user will use the default status of the tasks, therefore, it must be set
    // beforehand. Otherwise the value will not be saved in vuex.
    this.setTaskState(this.stateSelected)

    if (this.$route.params.name) {
      // Coming from `Management` view.
      this.taskName = this.$route.params.name

      const task = this.$store.getters['userTasks/tasks'].find( t => t.task.name === this.taskName)

      this.setActions(task.task.actions)
      this.setTrigger(task.task.trigger)
      this.setTaskState(task.task.state)
    }
  },
  components: {
    appElementSelector: ElementSelector,
    appElementsList: ElementsList
  }
}

</script>

<style lang="scss" scoped>
</style>
