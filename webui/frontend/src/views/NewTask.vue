<template>
<b-container>
  <div class="form m-1">
    <b-row>
      <b-col>
        <app-form-group-container
          containerTitle="Name"
          containerDescription="The name for your new task."
          topElementID="task-name"
          :withFooter="false"
        >
          <template v-slot:top>
            <input
              type="text"
              class="form-control"
              aria-describedby="task-name-description"
              placeholder="Enter a task name"
              id="task-name"
              v-model="taskName"
            />
          </template>
        </app-form-group-container>
      </b-col>
    </b-row>

    <b-row>
      <b-col>
        <app-form-group-container
          containerTitle="Default state"
          containerDescription="If the task will be executed (active) or not (inactive)."
          topElementID="default-state"
          :withFooter="true"
        >
          <template v-slot:top>
            <select id="default-state" class="form-control" v-model="stateSelected">
              <option>Active</option>
              <option>Inactive</option>
            </select>
          </template>
          <template v-slot:bottom>
            <b-button
              size="sm"
              :variant="setTaskstateBtnStyle"
              @click="setStateBtn"
            >{{ stateBtnTxt }}</b-button>
          </template>
        </app-form-group-container>
      </b-col>
    </b-row>

    <b-row>
      <b-col md="6">
        <app-form-group-container
          v-if="triggers.length > 0"
          containerTitle="Trigger"
          containerDescription="Select one trigger."
          topElementID="trigger-selector"
          :withFooter="true"
        >
          <template v-slot:top>
            <select
              id="trigger-selector"
              class="form-control"
              aria-describedby="trigger-selector-description"
              v-model="newTrigger"
            >
              <option
                v-for="trigger in triggers"
                :key="trigger.ID"
                :title="trigger.description"
              >{{ trigger.name }}</option>
            </select>
          </template>
          <template v-slot:bottom>
            <b-button
              size="sm"
              :variant="setTriggerBtnStyle"
              @click="addTriggerBtn"
            >{{ triggerBtnTxt }}</b-button>
          </template>
        </app-form-group-container>
        <p v-else class="text-center font-weight-bolder text-danger mt-2">Can't get the info of Triggers</p>
      </b-col>

      <b-col md="6">
        <app-form-group-container
          v-if="actions.length > 0"
          containerTitle="Actions"
          containerDescription="Select one or more actions."
          topElementID="actions-selector"
          :withFooter="true"
        >
          <template v-slot:top>
            <select
              id="actions-selector"
              class="form-control"
              aria-describedby="actions-selector-description"
              v-model="newAction"
            >
              <option
                v-for="action in actions"
                :key="action.ID"
                :title="action.description"
              >{{ action.name }}</option>
            </select>
          </template>
          <template v-slot:bottom>
            <b-button
              size="sm"
              :variant="addActionBtnStyle"
              @click="addActionBtn"
            >Add</b-button>
          </template>
        </app-form-group-container>
        <p v-else class="text-center font-weight-bolder text-danger mt-2">Can't get the info of Actions</p>
      </b-col>
    </b-row>
  </div>

  <div class="m-1">
    <b-row>
      <b-col>
        <app-summary/>
      </b-col>
    </b-row>
  </div>
  <b-button block variant="primary" class="m-1" :disabled="isAllSelected" @click="submitTask()">
    <span v-if="!submitted">Save</span>
    <b-spinner v-else variant="dark" class="" label="Loading"/>
  </b-button>
</b-container>
</template>

<script>
import Summary from '../components/new-task/Summary.vue'
import FormGroupContainer from '../components/new-task/FormGroupContainer.vue'
import SummaryCard from '../components/new-task/SummaryCard.vue'
import { mapMutations, mapGetters } from 'vuex'
import axios from 'axios'

export default {
  data () {
    return {
      newTrigger: '',
      newAction: '',
      stateSelected: '',
      submitted: false
    }
  },
  computed: {
    ...mapGetters('elementsInfo', [
      'triggers', 
      'actions'
    ]),
    taskName: {
      get() {
        return this.$store.getters['newTask/taskname']
      },
      set(newValue) {
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
    },
    triggerBtnTxt () {
      if (this.$store.getters['newTask/triggerSelected'].length > 0) {
        return 'Change'
      } else {
        return 'Select'
      }
    },
    stateBtnTxt () {
      if (this.$store.getters['newTask/taskState'] !== '') {
        return 'Change'
      } else {
        return 'Select'
      }
    },
    addActionBtnStyle () {
      if (!this.$store.getters['newTask/actionsSelected'].length > 0){
        return 'outline-primary'
      } else {
        return 'outline-success'
      }
    },
    setTriggerBtnStyle () {
      if (!this.$store.getters['newTask/triggerSelected'].length > 0){
        return 'outline-primary'
      } else {
        return 'outline-success'
      }
    },
    setTaskstateBtnStyle () {
      if (this.$store.getters['newTask/taskState'] == ''){
        return 'outline-primary'
      } else {
        return 'outline-success'
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
      let trigger = this.triggers.filter((t) => {
        return t.name == this.newTrigger
      })

      this.setTrigger(...trigger)
    },
    addActionBtn () {
      if (!this.newAction) {
        return
      }
      let action = this.actions.filter((a) => {
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
    submitTask () {
      if (this.submitted) return
      
      this.submitted = true
      console.info('Submitting a new task to the API...')
      const newTaskData = {
        'task': {
          'name': this.$store.getters["newTask/taskname"],
          'state': this.$store.getters["newTask/taskState"],
          // Only send one trigger. This is because, for now, multi-triggers are not supported.
          'trigger': this.$store.getters["newTask/triggerSelected"][0],
          'actions': this.$store.getters["newTask/actionsSelected"]
        }
      }

      // TODO Check the integrity of the data

      console.info("Sending the data to the new tasks's API")
      axios.post('/api/tasks/new', newTaskData)
        .then((response) => {
          if (response.data.successful) {
            // Show a success alert
          } else {
            // Show an error alert, showing the message received (response.data.error)
          }
          console.info('Data submitted correctly, response:', response)
          // Change the submitted variable only when the response is received
          this.submitted = false
        })
        .catch((err) => {
          console.error(err)
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
  components: {
    appSummary: Summary,
    appFormGroupContainer: FormGroupContainer
  }
}

</script>

<style lang="scss" scoped>
</style>