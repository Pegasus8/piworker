<template>
  <div
    class="modal fade"
    id="newTaskModal"
    tabindex="-1"
    role="dialog"
    aria-labelledby="modalTitle"
    aria-hidden="true"
  >
    <div class="modal-dialog modal-dialog-centered" role="document">
      <div class="modal-content">
        <div class="modal-header">
          <h4 class="modal-title text-capitalize" id="modalTitle">Create a new task</h4>
          <button type="button" class="close" data-dismiss="modal" aria-label="Close">
            <span aria-hidden="true">&times;</span>
          </button>
        </div>
        <div class="modal-body">
          <!-- SECTION Form -->
          <div class="form m-1">

            <app-form-group-container
              containerTitle="Name"
              containerDescription="The name for your new task."
              topElementID="task-name"
              :withFooter="false">
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

            <app-form-group-container
              containerTitle="Default state"
              containerDescription="If the task will be executed (active) or not (inactive)."
              topElementID="default-state"
              :withFooter="true">
              <template v-slot:top>
                <select 
                  id="default-state" 
                  class="form-control"
                  v-model="stateSelected"
                >
                  <option>Active</option>
                  <option>Inactive</option>
                </select>
              </template>
              <template v-slot:bottom>
                <button 
                  class="btn btn-sm"
                  :class="{ 
                    'btn-outline-primary': $store.getters.taskState == '',
                    'btn-outline-success': !$store.getters.taskState == ''
                  }"
                  @click="setStateBtn">
                  {{ stateBtnTxt }}
                </button>
              </template>
            </app-form-group-container>

            <app-form-group-container
              v-if="triggers.length > 0"
              containerTitle="Trigger"
              containerDescription="Select one trigger."
              topElementID="trigger-selector"
              :withFooter="true">
              <template v-slot:top>
                <select
                  id='trigger-selector'
                  class="form-control"
                  aria-describedby="trigger-selector-description"
                  v-model="newTrigger"
                >
                  <option v-for="trigger in triggers" :key="trigger.ID" :title="trigger.Description">
                    {{ trigger.Name }}
                  </option>
                </select>
              </template>
              <template v-slot:bottom>
                <button 
                  class="btn btn-sm"
                  :class="{ 
                    'btn-outline-primary': !$store.getters.triggerSelected.length > 0,
                    'btn-outline-success': $store.getters.triggerSelected.length > 0
                  }"
                  @click="addTriggerBtn">
                  {{ triggerBtnTxt }}
                </button>
              </template>
            </app-form-group-container>
            <p v-else class="text-center font-weight-bolder text-danger mt-2">
              Can't get the info of Triggers
            </p>

            <app-form-group-container
              v-if="actions.length > 0"
              containerTitle="Actions"
              containerDescription="Select one or more actions."
              topElementID="actions-selector"
              :withFooter="true">
              <template v-slot:top>
                <select
                  id="actions-selector"
                  class="form-control"
                  aria-describedby="actions-selector-description"
                  v-model="newAction">
                  <option v-for="action in actions" :key="action.ID" :title="action.Description">
                    {{ action.Name }}
                  </option>
                </select>
              </template>
              <template v-slot:bottom>
                <button 
                  class="btn btn-sm"
                  :class="{ 
                    'btn-outline-primary': !$store.getters.actionsSelected.length > 0,
                    'btn-outline-success': $store.getters.actionsSelected.length > 0
                  }"
                  @click="addActionBtn">
                  Add
                </button>
              </template>
            </app-form-group-container>
            <p v-else class="text-center font-weight-bolder text-danger mt-2">
              Can't get the info of Actions
            </p>

          </div> 

          <app-summary/>

        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" data-dismiss="modal">
            Close
          </button>
          <button type="button" class="btn btn-primary" :disabled='isAllSelected'>
            Save
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import Summary from './components/Summary.vue'
import FormGroupContainer from './components/FormGroupContainer.vue'
import SummaryCard from './components/SummaryCard.vue'
import { mapMutations } from 'vuex'
export default {
  data () {
    return {
      // TODO Obtain the elements from the API
      triggers: [
        {Name: "Trigger A", Description: "A random trigger", ID: 1, Args: [
          {ID: 'arg1', Name: 'Arg #1', Description: 'A simple arg', Content: '', ContentType: 'string'},
          {ID: 'arg2', Name: 'Arg #2', Description: 'A simple arg 2', Content: '', ContentType: 'string'}
        ]},
        {Name: "Trigger B", Description: "A random trigger", ID: 2, Args: [
          {ID: 'arg1', Name: 'Arg #1', Description: 'A simple arg', Content: '', ContentType: 'string'},
          {ID: 'arg2', Name: 'Arg #2', Description: 'A simple arg 2', Content: '', ContentType: 'string'}
        ]}
      ],
      actions: [
        {Name: "Action A2", Description: "A random action", ID: 10},
        {Name: "Action B", Description: "A random action", ID: 11}
      ],
      newTrigger: '',
      newAction: '',
      stateSelected: ''
    }
  },
  computed: {
    taskName: {
      get() {
        return this.$store.getters.taskname
      },
      set(newValue) {
        return this.$store.commit('setTaskname', newValue)
      }
    },
    isAllSelected () {
      if (this.taskName && this.stateSelected && 
        this.$store.getters.triggerSelected.length > 0 && 
        this.$store.getters.actionsSelected.length > 0) {
        return false
      } else {
        return true
      }
    },
    triggerBtnTxt () {
      if (this.$store.getters.triggerSelected.length > 0) {
        return 'Change'
      } else {
        return 'Select'
      }
    },
    stateBtnTxt () {
      if (this.$store.getters.taskState !== '') {
        return 'Change'
      } else {
        return 'Select'
      }
    }
  },
  methods: {
    ...mapMutations([
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
        return t.Name == this.newTrigger
      })

      this.setTrigger(...trigger)
    },
    addActionBtn () {
      if (!this.newAction) {
        return
      }
      let action = this.actions.filter((a) => {
        return a.Name === this.newAction
      })

      this.addAction(...action)
    },
    setStateBtn () {
      if (!this.stateSelected) {
        return
      }

      this.setTaskState(this.stateSelected)
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
