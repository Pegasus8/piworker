<template>
<v-container>
  <v-card>
    <v-card-title>
      Create a new task
    </v-card-title>
    <v-card-text>
      <v-form class="m-2" v-model="valid">
        <v-text-field
          v-model="taskName"
          :rules='taskNameRules'
          label="Task name"
          outlined
          required
        />

        <v-row justify='center'>
          <v-col cols='12' lg='6' align='center'>
            <v-autocomplete
              v-model='newTrigger'
              :items='triggers'
              label='Trigger'
              item-text='name'
              placeholder="Start typing to Search"
              hide-no-data
              hide-selected
              outlined
              return-object
            />
            <!-- TODO Implementation of args selection and triggers list -->
            {{ $store.getters['newTask/triggerSelected'] }}
          </v-col>
          <v-col cols='12' lg='6' align='center'>
            <v-autocomplete
              v-model='newAction'
              :items='actions'
              label='Actions'
              item-text='name'
              placeholder="Start typing to Search"
              hide-no-data
              hide-selected
              outlined
              return-object
            />
            <!-- TODO Implementation of args selection and actions list -->
            {{ $store.getters['newTask/actionsSelected'] }}
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
</v-container>
</template>

<script>
import { mapMutations, mapGetters } from 'vuex'
import router from '../router'

export default {
  data () {
    return {
      valid: false,
      taskNameRules: [
        v => !v || 'The task must have a name'
        // TODO Check if the name of the task is not repeated.
      ],
      selectTriggerRules: [],
      selectActionsRules: [],

      newTrigger: '',
      newAction: '',
      stateSelected: true,
      submitted: false,
      alert: false,
      alertVariant: 'success',
      responseContent: ''
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
      'addAction'
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
  }
}

</script>

<style lang="scss" scoped>
</style>
