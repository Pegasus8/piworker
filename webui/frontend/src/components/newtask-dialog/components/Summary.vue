<template>
<div class="card bg-dark text-white" id="summary-card">
  <div class="card-header font-weight-bold">
    Summary
  </div>

  <div class="card-body p-3">

    <app-summary-card 
      cardTitle="Name of the task"
      :contentToEvaluate="taskName">
      {{ taskName }}
    </app-summary-card>

    <app-summary-card
      cardTitle="Default state"
      :contentToEvaluate="taskState">
      {{ taskState }}
    </app-summary-card>

    <app-summary-card
      cardTitle="Trigger selected"
      :contentToEvaluate="triggers"
      @switchChange="canDragTriggers = $event">
      <draggable 
        v-model="triggers"
        v-if="triggers.length > 0"
        :disabled="!canDragTriggers"
        @start="drag=true" 
        @end="drag=false"
        class="list-group">
        <!-- NOTE For trigger parsing, is unnecessary do a for loop
        due to there is a only one trigger per task. This was made
        in this way for a future implementation of multiple triggers. -->
        <div
          v-for="(userTrigger, index) in triggers" 
          :key="userTrigger.ID + '_' + $uuid.v1()" 
          :title="userTrigger.Description"
          class="list-group-item text-break text-bolder text-dark">
          {{ userTrigger.Name }}
          <button 
            type="button" 
            class="close" 
            aria-label="Remove trigger"
            @click="removeTrigger(index)">
            <span aria-hidden="true">&times;</span>
          </button>
          <div class="border rounded m-2 p-1 row">
            
            <div 
              class="col-10 col-md-6 mx-auto my-2"
              v-for="arg in userTrigger.Args" :key="userTrigger.ID + arg.ID">
              <div class="card bg-light" :title="arg.Description">
                <div class="h5 p-1 card-header text-wrap">
                  {{ arg.Name }}
                </div>
                <div class="card-body text-wrap">
                  <input 
                    type="text" 
                    class="form-control" 
                    placeholder="Content" 
                    aria-label="Argument content" 
                    v-model.lazy="arg.Content">
                </div>
              </div>
            </div>
            
          </div>
        </div>
      </draggable>
    </app-summary-card>
    
    <app-summary-card
      cardTitle="Actions selected"
      :contentToEvaluate="actions"
      @switchChange="canDragActions = $event">
      <draggable 
        v-model="actions"
        :disabled="!canDragActions"
        @start="drag=true" 
        @end="drag=false"
        class="list-group">
        <div 
          v-for="(userAction, index) in actions" 
          :key="userAction.ID + '_' + $uuid.v1()" 
          :title="userAction.Description"
          class="list-group-item text-break text-bolder text-dark">
          {{ userAction.Name }}
          <button 
            type="button" 
            class="close" 
            aria-label="Remove action"
            @click="removeAction(index)">
            <span aria-hidden="true">&times;</span>
          </button>
          <div class="border rounded m-2 p-1 row">
            
            <div 
              class="col-10 col-md-6 mx-auto my-2"
              v-for="(arg, argIndex) in userAction.Args" :key="arg.ID">
              <div class="card bg-light" :title="arg.Description">
                <div class="h5 p-1 card-header text-wrap">
                  {{ arg.Name }}
                </div>
                <div class="card-body text-wrap">
                  <input 
                    type="text" 
                    class="form-control" 
                    placeholder="Content" 
                    aria-label="Argument content" 
                    @change="$store.commit('setActionArgContent', {
                      actionIndex: index, 
                      argumentIndex: argIndex, 
                      contentToSet: $event.target.value
                    })">
                </div>
              </div>
            </div>
            
          </div>
        </div>
      </draggable>
      <small class="text-muted">Tip: drag and drop for order the actions</small>
    </app-summary-card>
    
  </div>

</div>


</template>

<script>
import draggable from 'vuedraggable'
import { mapMutations } from 'vuex'
import SummaryCard from './SummaryCard.vue'

export default {
  data () {
    return {
      canDragActions: true,
      canDragTriggers: true
    }
  },
  computed: {
    taskName() {
      return this.$store.getters.taskname
    },
    taskState() {
      return this.$store.getters.taskState
    },
    triggers: {
      get() {
        return this.$store.getters.triggerSelected
      },
      set(newValue) {
        this.$store.commit('setTrigger', newValue)
      }
    },
    actions: {
      get() {
        return this.$store.getters.actionsSelected
      },
      set(newValue) {
        this.$store.commit('setActions', newValue)
      }
    }
  },
  methods: {
    ...mapMutations([
      'setTrigger', 'removeTrigger',
      'setActions', 'removeAction'
    ])
  },
  components: {
    draggable,
    appSummaryCard: SummaryCard
  },
  filters: {
  }
}
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}
.summary {
  background-color: rgba(221, 221, 221, 0.411);
}
#summary-card, .card {
  // background-color: rgb(44, 49, 54),
  background: rgb(36, 40, 44)
}
</style>