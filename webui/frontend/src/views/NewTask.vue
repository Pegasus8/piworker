<template>
<v-container>
  <v-card>
    <v-card-title>
      Create a new task
    </v-card-title>
    <v-card-text>
      <v-form class="m-2" v-model="valid" ref="form">
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
              inset
            />
          </v-col>
        </v-row>
      </v-form>
    </v-card-text>
  </v-card>

  <v-btn
    class="primary darken-2 m-1"
    :disabled="!valid"
    :loading='submitted'
    @click="submitTask()"
    block
  >
    Save
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
      valid: false,
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
        return false
      } else {
        return true
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
    addTriggerBtn () {
      if (!this.newTrigger) {
        return
      }
      const trigger = this.triggers.filter((t) => {
        return t.name === this.newTrigger
      })

      this.setTrigger(...trigger)
    },
    addActionBtn () {
      if (!this.newAction) {
        return
      }
      const action = this.actions.filter((a) => {
        return a.name === this.newAction
      })

      this.addAction(...action)
    },
    setStateBtn () {
      if (!this.stateSelected) {
        return
      }

      this.setTaskState(this.stateSelected)
    },
    clearFields () {
      this.taskName = ''
      this.stateSelected = ''
      this.setTaskState('')
      this.newTrigger = ''
      this.newAction = ''
      this.$store.commit('newTask/setActions', [])
      this.$store.commit('newTask/setTrigger', null) // cambiar
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
  beforeCreate () {
    if (!this.$store.getters['elementsInfo/triggers'].length > 0) {
      this.$store.dispatch('elementsInfo/updateTriggersInfo')
    }
    if (!this.$store.getters['elementsInfo/actions'].length > 0) {
      this.$store.dispatch('elementsInfo/updateActionsInfo')
    }
  },
  watch: {
    newAction: function (newVal) {
      if (!newVal) return // Prevent the execution when we change the variable `this.newAction` to null.
      this.addAction(newVal)
      this.newAction = null
    },
    newTrigger: function (newVal) {
      if (!newVal) return // Prevent the execution when we change the variable `this.newTrigger` to null.
      this.setTrigger(newVal)
      this.newTrigger = null
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
