<template>
  <div class="card bg-dark border border-secondary text-white">
    <div class="card-header text-left">
      <div class="d-flex">
        <div class="flex-grow-1 font-weight-bold">{{ taskName }}</div>
        <router-link class="icon-pencil mx-3" to=""/>
        <div class="custom-control custom-switch">
          <input
            type="checkbox"
            class="custom-control-input"
            :id="'customSwitch' + _uid"
            v-model="state"
          />
          <label class="custom-control-label" :for="'customSwitch' + _uid"></label>
        </div>
      </div>
    </div>
    <div class="card-body">

      <div class="card bg-dark text-white mb-3">
        <div class="card-header">
          Triggers
        </div>
        <div class="card-body">
          <div class="container">
            <ul class="list-group bg-dark" v-if="triggers.length > 0">
              <app-list-group-item
                v-for="trigger in triggers"
                :key="trigger.ID + $uuid.v1()"
                :editable="false"
                :itemName="trigger.Name"
                :args="trigger.Args"
              />
            </ul>
          </div>
        </div>
      </div>

      <div class="card bg-dark text-white">
        <div class="card-header">Actions</div>
        <div class="card-body">
          <div class="container">
            <ul class="list-group bg-dark" v-if="actions.length > 0">
              <app-list-group-item
                v-for="action in actions" 
                :key="action.ID + $uuid.v1()"
                :editable="false"
                :itemName="action.Name"
                :args="action.Args"
              />
            </ul>
          </div>
        </div>
      </div>

    </div>
    <div class="card-footer" >

      <div class="accordion" :id="divFooterAccordionID">
        <div :id="logsHeadingID">
          <button 
            class="btn btn-link" 
            type="button" 
            data-toggle="collapse" 
            :data-target="'#' + divLogsCollapseID" 
            aria-expanded="true" 
            :aria-controls="divLogsCollapseID">
            Logs
          </button>
        </div>
        <div 
          :id="divLogsCollapseID" 
          class="collapse p-2 text-wrap text-monospace text-left small" 
          :aria-labelledby="logsHeadingID" 
          :data-parent="'#' + divFooterAccordionID">
          {{ logs }}
        </div>
      </div>
      
    </div>
  </div>
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
      type: Boolean
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
      get() {
        return this.taskState;
      },
      set(newValue) {
        this.$emit("switch-change", newValue);
      }
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