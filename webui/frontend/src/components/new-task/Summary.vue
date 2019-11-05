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
          :title="userTrigger.description"
          class="list-group-item">
          <div class="d-flex">
            <div class="flex-grow-1 text-break text-bolder text-dark">
              {{ userTrigger.name }}
              <!-- TODO Show info -->
              <router-link
                tag="span"
                class="icon-info mx-1"
                to=""
              />
            </div>
            <!-- Switch -->
          </div>
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
              v-for="arg in userTrigger.args" :key="arg.ID + '_' + $uuid.v1()">
              <div class="card bg-light" :title="arg.description">
                <div class="h5 p-1 card-header text-wrap">
                  {{ arg.name }}
                </div>
                <div class="card-body text-wrap">
                  <input 
                    type="text" 
                    class="form-control" 
                    placeholder="Content" 
                    aria-label="Argument content" 
                    v-model.lazy="arg.content">
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
          :title="userAction.description"
          class="list-group-item text-break text-bolder text-dark">
          {{ userAction.name }}
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
              v-for="arg in userAction.args" :key="arg.ID + '_' + $uuid.v1()">
              <div class="card bg-light" :title="arg.description">
                <div class="h5 p-1 card-header text-wrap">
                  {{ arg.name }}
                </div>
                <div class="card-body text-wrap">
                  <input 
                    type="text" 
                    class="form-control" 
                    placeholder="Content" 
                    aria-label="Argument content"
                    v-model.lazy="arg.content">
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
      return this.$store.getters['newTask/taskname']
    },
    taskState() {
      return this.$store.getters['newTask/taskState']
    },
    triggers: {
      get() {
        return this.$store.getters['newTask/triggerSelected']
      },
      set(newValue) {
        this.$store.commit('newTask/setTrigger', newValue)
      }
    },
    actions: {
      get() {
        return this.$store.getters['newTask/actionsSelected']
      },
      set(newValue) {
        this.$store.commit('newTask/setActions', newValue)
      }
    }
  },
  methods: {
    ...mapMutations('newTask', [
      'setTrigger', 'removeTrigger',
      'setActions', 'removeAction'
    ])
  },
  components: {
    draggable,
    appSummaryCard: SummaryCard
  },
  filters: {
  },
  updated () {
    for (const [index, action] of this.actions.entries()) {
      action.order = index
    }
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