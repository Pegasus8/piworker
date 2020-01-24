<template>
  <b-card bg-variant="dark" border-variant="secondary" text-variant="light" no-body>

    <b-card-header class="text-left">
      <div class="d-flex">
        <div class="flex-grow-1 font-weight-bold">{{ taskName }}</div>
        <!-- TODO Add functionality of task edition -->
        <router-link class="icon-pencil mx-3" to=""/>
        <b-form-checkbox
          :id="'customSwitch' + _uid"
          v-model="state"
          switch
        ><!-- Checkbox description here --></b-form-checkbox>
      </div>
    </b-card-header>
    <b-card-body>

      <b-card bg-variant="dark" text-variant="light" class="mb-3" no-body>
        <b-card-header>
          Triggers
        </b-card-header>
        <b-card-body>
          <b-container>
            <b-list-group variant="dark" v-if="triggers.length > 0">
              <app-list-group-item
                v-for="trigger in triggers"
                :key="trigger.ID + $uuid.v1()"
                :editable="false"
                :itemName="getTriggerName(trigger.ID)"
                :args="setTriggerArgsNames(trigger.ID, trigger.args)"
              />
            </b-list-group>
          </b-container>
        </b-card-body>
      </b-card>

     <b-card bg-variant="dark" text-variant="light" no-body>
        <b-card-header>Actions</b-card-header>
        <b-card-body>
          <b-container>
            <b-list-group variant="dark" v-if="actions.length > 0">
              <app-list-group-item
                v-for="action in actions"
                :key="action.ID + $uuid.v1()"
                :editable="false"
                :itemName="getActionName(action.ID)"
                :args="setActionArgsNames(action.ID, action.args)"
              />
            </b-list-group>
          </b-container>
        </b-card-body>
      </b-card>

    </b-card-body>
    <b-card-footer>

      <div :id="divFooterAccordionID">
        <div :id="logsHeadingID">
          <b-button
            v-b-toggle="divLogsCollapseID"
            variant="link"
          >
            Logs
          </b-button>
        </div>
        <b-collapse
          accordion="logs-accordion"
          :id="divLogsCollapseID"
          class="p-2 text-wrap text-monospace text-left small"
        >
          {{ logs }}
        </b-collapse>
      </div>

    </b-card-footer>
  </b-card>
</template>

<script>
import ListGroupItem from './components/ListGroupItem.vue'
export default {
  data () {
    return {
      divFooterAccordionID: 'accordion-' + this.$uuid.v1(),
      divLogsCollapseID: 'collapse-' + this.$uuid.v1(),
      logsHeadingID: 'logs-heading-' + this.$uuid.v1()
    }
  },
  props: {
    taskName: {
      required: true,
      type: String
    },
    taskState: {
      required: true,
      type: String
    },
    triggers: {
      required: true,
      type: Array
    },
    actions: {
      required: true,
      type: Array
    },
    logs: {
      required: false,
      type: String,
      default: ''
    }
  },
  computed: {
    state: {
      get () {
        if (this.taskState === 'active') return true
        else return false
      },
      set (newValue) {
        let value
        if (newValue) value = 'active'
        else value = 'inactive'

        this.$emit('switch-change', value)
      }
    }
  },
  methods: {
    getTriggerName (id) {
      this.$store.getters['elementsInfo/triggers'].find((trigger) => {
        if (trigger.ID === id) return trigger.name
      })
    },
    getActionName (id) {
      this.$store.getters['elementsInfo/actions'].find((action) => {
        if (action.ID === id) return action.name
      })
    },
    setTriggerArgsNames (userTriggerID, userTriggerArgs) {
      this.$store.getters['elementsInfo/triggers'].find((trigger) => {
        if (trigger.ID === userTriggerID) {
          for (const arg in trigger.Args) {
            userTriggerArgs.find((userArg) => {
              if (arg.ID === userArg.ID) {
                // Set the name to the user arg.
                userArg.name = arg.name
              }
            })
          }
        }
      })

      return userTriggerArgs
    },
    setActionArgsNames (userActionID, userActionArgs) {
      this.$store.getters['elementsInfo/actions'].find((action) => {
        if (action.ID === userActionID) {
          for (const arg in action.args) {
            userActionArgs.find((userArg) => {
              if (arg.ID === userArg.ID) {
                // Set the name to the user arg.
                userArg.name = arg.name
              }
            })
          }
        }
      })

      return userActionArgs
    }
  },
  components: {
    appListGroupItem: ListGroupItem
  }
}
</script>

<style lang="scss" scoped>
[class^="icon-"] {
  opacity: 0.3;
  color: #FFFFFF;
  text-decoration: none;
}
</style>
